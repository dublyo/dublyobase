package core

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A minimal .xlsx writer. The format is a zip of XML parts, and the subset
// needed for one sheet of typed cells is small — so this is written directly
// rather than pulling in a spreadsheet library. The runtime is deliberately
// four dependencies, and an export format does not justify a fifth.
//
// Cells are typed: anything that parses as a plain decimal is written as a
// number so Excel can sum it, everything else as an inline string. Inline
// strings avoid the sharedStrings part entirely.

const maxXLSXColumns = 16384

func xlsxColumnName(index int) string {
	name := ""
	for index >= 0 {
		name = string(rune('A'+index%26)) + name
		index = index/26 - 1
	}
	return name
}

// numericCell reports whether a value should be written as a number. Excel
// stores every number as a float64, so very long decimals are kept as text
// rather than silently rounded in a spreadsheet someone will total.
func numericCell(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || len(v) > 15 {
		return false
	}
	if _, err := strconv.ParseFloat(v, 64); err != nil {
		return false
	}
	// keep leading-zero identifiers (phone numbers, codes) as text
	if len(v) > 1 && (v[0] == '0' && v[1] != '.') {
		return false
	}
	if strings.ContainsAny(v, "eE") {
		return false
	}
	return true
}

type xlsxWriter struct {
	zip   *zip.Writer
	sheet io.Writer
	rows  int
	err   error
}

func newXLSXWriter(w io.Writer) (*xlsxWriter, error) {
	zw := zip.NewWriter(w)
	write := func(name, body string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	if err := write("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
		`<Default Extension="xml" ContentType="application/xml"/>`+
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`+
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`+
		`</Types>`); err != nil {
		return nil, err
	}
	if err := write("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>`+
		`</Relationships>`); err != nil {
		return nil, err
	}
	if err := write("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" `+
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
		`<sheets><sheet name="Export" sheetId="1" r:id="rId1"/></sheets></workbook>`); err != nil {
		return nil, err
	}
	if err := write("xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>`+
		`</Relationships>`); err != nil {
		return nil, err
	}
	sheet, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(sheet, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return nil, err
	}
	return &xlsxWriter{zip: zw, sheet: sheet}, nil
}

func (x *xlsxWriter) Write(cells []string) error {
	if x.err != nil {
		return x.err
	}
	if len(cells) > maxXLSXColumns {
		x.err = fmt.Errorf("%w: a sheet cannot have more than %d columns", ErrValidation, maxXLSXColumns)
		return x.err
	}
	x.rows++
	var b strings.Builder
	fmt.Fprintf(&b, `<row r="%d">`, x.rows)
	for i, cell := range cells {
		ref := xlsxColumnName(i) + strconv.Itoa(x.rows)
		if cell == "" {
			continue
		}
		if numericCell(cell) {
			fmt.Fprintf(&b, `<c r="%s"><v>%s</v></c>`, ref, strings.TrimSpace(cell))
			continue
		}
		var escaped strings.Builder
		// xml.EscapeText also handles the control characters Excel rejects
		if err := xml.EscapeText(&escaped, []byte(stripXMLIncompatible(cell))); err != nil {
			x.err = err
			return err
		}
		fmt.Fprintf(&b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, escaped.String())
	}
	b.WriteString(`</row>`)
	_, x.err = io.WriteString(x.sheet, b.String())
	return x.err
}

func (x *xlsxWriter) Close() error {
	if x.err != nil {
		return x.err
	}
	if _, err := io.WriteString(x.sheet, `</sheetData></worksheet>`); err != nil {
		return err
	}
	return x.zip.Close()
}

// stripXMLIncompatible removes control characters XML 1.0 forbids. They can
// reach here from imported data, and Excel refuses to open a file containing
// them, reporting only that the workbook is corrupt.
func stripXMLIncompatible(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
			return -1
		}
		return r
	}, s)
}

// ExportRecordsXLSX streams the collection as a real .xlsx workbook.
func ExportRecordsXLSX(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, opts RecordExportOptions, w io.Writer) (int, error) {
	sheet, err := newXLSXWriter(w)
	if err != nil {
		return 0, err
	}
	count, err := ExportRows(ctx, pool, auth, collectionName, opts, sheet.Write, sheet.Write)
	if err != nil {
		return count, err
	}
	return count, sheet.Close()
}

// ExportAggregateXLSX writes a grouped aggregate as a workbook.
func ExportAggregateXLSX(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, input AggregateInput, w io.Writer) (int, error) {
	sheet, err := newXLSXWriter(w)
	if err != nil {
		return 0, err
	}
	count, err := exportAggregateTo(ctx, pool, auth, collectionName, input, sheet.Write, sheet.Write)
	if err != nil {
		return count, err
	}
	return count, sheet.Close()
}
