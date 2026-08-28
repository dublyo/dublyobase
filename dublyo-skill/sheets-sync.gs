/**
 * Dublyobase → Google Sheets
 * ---------------------------------------------------------------------------
 * A one-way reporting sync. Postgres stays authoritative; the Sheet is a window
 * onto it, refreshed on demand or on a timer, and styled on every refresh so it
 * never drifts back to a raw grey grid.
 *
 * SETUP
 *   1. Extensions → Apps Script, paste this file, save.
 *   2. Project Settings → Script Properties, add:
 *        DUBLYO_URL   https://your-instance.example.com
 *        DUBLYO_KEY   the API key
 *        DUBLYO_PROJ  your project slug
 *   3. Reload the Sheet. A "Dublyo" menu appears. Run "Refresh all".
 *
 * ABOUT THE KEY — read this before sharing the Sheet
 *   Anyone who can EDIT this Sheet can open Apps Script and read the key. It is
 *   not a secret from editors. So:
 *     · a SERVICE key reads everything in the project, rules bypassed. Only use
 *       one in a Sheet you do not share, or share read-only.
 *     · an ANON key is subject to each collection's listRule. Prefer this, and
 *       open listRule on just the collections meant for reporting. Then a leaked
 *       Sheet exposes only what you deliberately published.
 *   Give viewers "Viewer" access, not "Editor", and they cannot reach the key.
 */

// ── What to sync ────────────────────────────────────────────────────────────
// `fields` accepts dotted paths that walk many-to-one relations, so a flat tab
// can pull columns from across the schema.
const CONFIG = {
  tabs: [
    {
      name: 'Patients',
      collection: 'patients',
      fields: 'mrn,full_name,phone,email,status,primary_provider.full_name,home_location.name,source_campaign.name,lifetime_value',
      sort: 'mrn',
      money: ['lifetime_value'],
      pills: { status: { active: 'good', prospect: 'info', inactive: 'muted', discharged: 'muted' } },
    },
    {
      name: 'Appointments',
      collection: 'appointments',
      fields: 'starts_at,status,kind,patient.full_name,provider.full_name,location.name,service.name,duration_minutes',
      sort: '-starts_at',
      dates: ['starts_at'],
      pills: {
        status: { completed: 'good', confirmed: 'good', scheduled: 'info',
                  in_progress: 'info', cancelled: 'bad', no_show: 'bad' },
      },
    },
    {
      name: 'Pipeline',
      collection: 'leads',
      fields: 'full_name,phone,status,score,estimated_value,campaign.name,service_interest,owner.full_name',
      sort: '-score',
      money: ['estimated_value'],
      pills: {
        status: { converted: 'good', consulted: 'info', consult_booked: 'info',
                  contacted: 'muted', new: 'muted', lost: 'bad' },
      },
    },
    {
      name: 'Payments',
      collection: 'payments',
      fields: 'paid_at,patient.full_name,invoice.invoice_no,amount,method,kind',
      sort: '-paid_at',
      money: ['amount'],
      dates: ['paid_at'],
    },
  ],
  // Grouped totals computed in Postgres. Never build these by joining tabs in
  // the Sheet: joining two one-to-many relations multiplies every row by the
  // other side, and the wrong number looks entirely plausible.
  summaries: [
    { name: 'Sum · Pipeline', collection: 'leads', aggregate: 'count:*,sum:estimated_value', groupBy: 'status' },
    { name: 'Sum · Revenue', collection: 'payments', aggregate: 'count:*,sum:amount', groupBy: 'method' },
    { name: 'Sum · Appointments', collection: 'appointments', aggregate: 'count:*', groupBy: 'status' },
  ],
};

// ── Theme ───────────────────────────────────────────────────────────────────
const THEME = {
  headerBg: '#16607a',
  headerFg: '#ffffff',
  band: '#f2f6f8',
  border: '#cfd9de',
  font: 'Inter',
  good: { bg: '#d8ece2', fg: '#125136' },
  info: { bg: '#dceef4', fg: '#0f4c60' },
  muted: { bg: '#eceeef', fg: '#5a666d' },
  bad: { bg: '#f6dedb', fg: '#8a2b23' },
  kwd: '#,##0.000',
  date: 'dd mmm yyyy  hh:mm',
  maxColWidth: 260,
};

function onOpen() {
  SpreadsheetApp.getUi()
    .createMenu('Dublyo')
    .addItem('Refresh all', 'refreshAll')
    .addItem('Refresh this tab', 'refreshCurrentTab')
    .addSeparator()
    .addItem('Rebuild dashboard', 'buildDashboard')
    .addItem('Install hourly refresh', 'installTrigger')
    .addItem('Remove hourly refresh', 'removeTrigger')
    .addToUi();
}

// ── Sync ────────────────────────────────────────────────────────────────────
function refreshAll() {
  const started = Date.now();
  let synced = 0;
  CONFIG.tabs.forEach(function (spec) { syncTab_(spec); synced++; });
  CONFIG.summaries.forEach(function (spec) { syncSummary_(spec); synced++; });
  buildDashboard();
  SpreadsheetApp.getActive().toast(
    synced + ' tabs in ' + Math.round((Date.now() - started) / 1000) + 's', 'Dublyo', 5);
}

function refreshCurrentTab() {
  const name = SpreadsheetApp.getActiveSheet().getName();
  const tab = CONFIG.tabs.filter(function (t) { return t.name === name; })[0];
  const sum = CONFIG.summaries.filter(function (t) { return t.name === name; })[0];
  if (tab) { syncTab_(tab); }
  else if (sum) { syncSummary_(sum); }
  else { SpreadsheetApp.getActive().toast('This tab is not synced.', 'Dublyo', 4); return; }
  SpreadsheetApp.getActive().toast('Refreshed ' + name, 'Dublyo', 4);
}

function syncTab_(spec) {
  const rows = fetchCSV_('/collections/' + spec.collection + '/records/export', {
    fields: spec.fields || '', sort: spec.sort || '', limit: String(spec.limit || 5000),
  });
  writeAndStyle_(spec.name, rows, spec);
}

function syncSummary_(spec) {
  const rows = fetchCSV_('/collections/' + spec.collection + '/records/aggregate/export', {
    aggregate: spec.aggregate, groupBy: spec.groupBy || '',
  });
  // every numeric column in a summary is worth formatting
  const money = rows.length ? rows[0].filter(function (h) { return /^(sum|avg|min|max)_/.test(h); }) : [];
  writeAndStyle_(spec.name, rows, { money: money });
}

// ── Transport ───────────────────────────────────────────────────────────────
function fetchCSV_(path, params) {
  const props = PropertiesService.getScriptProperties();
  const base = (props.getProperty('DUBLYO_URL') || '').replace(/\/+$/, '');
  const key = props.getProperty('DUBLYO_KEY');
  const proj = props.getProperty('DUBLYO_PROJ');
  if (!base || !key || !proj) {
    throw new Error('Set DUBLYO_URL, DUBLYO_KEY and DUBLYO_PROJ in Project Settings → Script Properties.');
  }
  const query = Object.keys(params)
    .filter(function (k) { return params[k]; })
    .map(function (k) { return encodeURIComponent(k) + '=' + encodeURIComponent(params[k]); })
    .join('&');
  const url = base + '/api/projects/' + encodeURIComponent(proj) + path + '?' + query;

  const response = UrlFetchApp.fetch(url, {
    headers: { Authorization: 'Bearer ' + key },
    muteHttpExceptions: true,
    followRedirects: true,
  });
  const code = response.getResponseCode();
  if (code !== 200) {
    // The API reports a bad field path or filter as JSON, so surface its own
    // message rather than a generic failure.
    let detail = response.getContentText().slice(0, 300);
    try { detail = JSON.parse(detail).message || detail; } catch (e) {}
    throw new Error('Dublyo ' + code + ': ' + detail);
  }
  // The export leads with a UTF-8 BOM so Excel reads Arabic correctly; strip it
  // before parsing or it becomes part of the first header.
  const text = response.getContentText('UTF-8').replace(/^﻿/, '');
  return Utilities.parseCsv(text);
}

// ── Write + style ───────────────────────────────────────────────────────────
function writeAndStyle_(name, rows, spec) {
  const ss = SpreadsheetApp.getActive();
  let sheet = ss.getSheetByName(name) || ss.insertSheet(name);
  sheet.clear();
  sheet.clearConditionalFormatRules();
  getBandings_(sheet).forEach(function (b) { b.remove(); });

  if (!rows.length) {
    sheet.getRange(1, 1).setValue('No data').setFontColor('#8a9299');
    return;
  }
  const cols = rows[0].length;
  sheet.getRange(1, 1, rows.length, cols).setValues(rows);

  // header
  const header = sheet.getRange(1, 1, 1, cols);
  header.setBackground(THEME.headerBg).setFontColor(THEME.headerFg)
    .setFontWeight('bold').setFontSize(10).setVerticalAlignment('middle');
  sheet.setFrozenRows(1);
  sheet.setRowHeight(1, 34);

  const body = rows.length > 1 ? sheet.getRange(2, 1, rows.length - 1, cols) : null;
  if (body) {
    body.setFontFamily(THEME.font).setFontSize(10).setVerticalAlignment('middle');
    body.applyRowBanding(SpreadsheetApp.BandingTheme.LIGHT_GREY, false, false);
  }
  sheet.getRange(1, 1, rows.length, cols).setBorder(
    true, true, true, true, true, true, THEME.border, SpreadsheetApp.BorderStyle.SOLID);

  const headers = rows[0];
  const index = {};
  headers.forEach(function (h, i) { index[h] = i + 1; });

  // Money right-aligned with a fixed 3-decimal format. KWD has three decimals,
  // so the default "1234.5" would read as a different amount to a fils.
  (spec.money || []).forEach(function (col) {
    if (!index[col] || !body) return;
    sheet.getRange(2, index[col], rows.length - 1, 1)
      .setNumberFormat(THEME.kwd).setHorizontalAlignment('right');
  });
  (spec.dates || []).forEach(function (col) {
    if (!index[col] || !body) return;
    sheet.getRange(2, index[col], rows.length - 1, 1).setNumberFormat(THEME.date);
  });

  // Status pills, so state reads at a glance instead of being scanned.
  const rules = [];
  Object.keys(spec.pills || {}).forEach(function (col) {
    if (!index[col] || !body) return;
    const range = sheet.getRange(2, index[col], rows.length - 1, 1);
    Object.keys(spec.pills[col]).forEach(function (value) {
      const tone = THEME[spec.pills[col][value]] || THEME.muted;
      rules.push(SpreadsheetApp.newConditionalFormatRule()
        .whenTextEqualTo(value)
        .setBackground(tone.bg).setFontColor(tone.fg).setBold(true)
        .setRanges([range]).build());
    });
  });
  if (rules.length) sheet.setConditionalFormatRules(rules);

  // Arabic and other RTL text needs the cell aligned right to read correctly;
  // the sheet itself stays LTR so English columns are unaffected.
  if (body) {
    for (let c = 1; c <= cols; c++) {
      const sample = rows.length > 1 ? String(rows[1][c - 1] || '') : '';
      if (/[֐-ࣿ]/.test(sample)) {
        sheet.getRange(2, c, rows.length - 1, 1).setHorizontalAlignment('right');
      }
    }
  }

  sheet.autoResizeColumns(1, cols);
  for (let c = 1; c <= cols; c++) {
    if (sheet.getColumnWidth(c) > THEME.maxColWidth) sheet.setColumnWidth(c, THEME.maxColWidth);
  }
  if (rows.length > 1) sheet.getRange(1, 1, rows.length, cols).createFilter();
}

function getBandings_(sheet) {
  try { return sheet.getBandings(); } catch (e) { return []; }
}

// ── Dashboard ───────────────────────────────────────────────────────────────
function buildDashboard() {
  const ss = SpreadsheetApp.getActive();
  let sheet = ss.getSheetByName('Dashboard') || ss.insertSheet('Dashboard', 0);
  sheet.clear();
  sheet.getCharts().forEach(function (c) { sheet.removeChart(c); });
  sheet.setHiddenGridlines(true);

  sheet.getRange('B2').setValue('Clinic overview')
    .setFontSize(20).setFontWeight('bold').setFontColor('#16202a');
  sheet.getRange('B3').setValue('Refreshed ' + Utilities.formatDate(new Date(), Session.getScriptTimeZone(), 'd MMM yyyy HH:mm'))
    .setFontColor('#6f8291').setFontSize(10);

  const kpis = [
    ['Leads', "=IFERROR(COUNTA(Pipeline!A2:A),0)"],
    ['Converted', "=IFERROR(COUNTIF(Pipeline!C2:C,\"converted\"),0)"],
    ['Patients', "=IFERROR(COUNTA(Patients!A2:A),0)"],
    ['Collected', "=IFERROR(SUM(Payments!D2:D),0)"],
  ];
  kpis.forEach(function (kpi, i) {
    const col = 2 + i * 2;
    sheet.getRange(5, col).setValue(kpi[0])
      .setFontSize(9).setFontColor('#6f8291').setFontWeight('bold');
    const value = sheet.getRange(6, col).setFormula(kpi[1])
      .setFontSize(22).setFontWeight('bold').setFontColor(THEME.headerBg);
    if (kpi[0] === 'Collected') value.setNumberFormat(THEME.kwd);
    sheet.getRange(5, col, 2, 2).setBackground('#f7fafb')
      .setBorder(true, true, true, true, false, false, THEME.border, SpreadsheetApp.BorderStyle.SOLID);
  });

  const pipeline = ss.getSheetByName('Sum · Pipeline');
  if (pipeline && pipeline.getLastRow() > 1) {
    sheet.insertChart(sheet.newChart().asColumnChart()
      .addRange(pipeline.getRange(1, 1, pipeline.getLastRow(), 2))
      .setPosition(9, 2, 0, 0).setOption('title', 'Leads by stage')
      .setOption('legend', { position: 'none' }).setOption('colors', [THEME.headerBg])
      .setOption('width', 480).setOption('height', 280).build());
  }
  const revenue = ss.getSheetByName('Sum · Revenue');
  if (revenue && revenue.getLastRow() > 1) {
    sheet.insertChart(sheet.newChart().asPieChart()
      .addRange(revenue.getRange(1, 1, revenue.getLastRow(), 1))
      .addRange(revenue.getRange(1, 3, revenue.getLastRow(), 1))
      .setPosition(9, 7, 0, 0).setOption('title', 'Revenue by method')
      .setOption('pieHole', 0.45).setOption('width', 480).setOption('height', 280).build());
  }
  sheet.setColumnWidth(1, 30);
  ss.setActiveSheet(sheet);
}

// ── Scheduling ──────────────────────────────────────────────────────────────
function installTrigger() {
  removeTrigger();
  ScriptApp.newTrigger('refreshAll').timeBased().everyHours(1).create();
  SpreadsheetApp.getActive().toast('Hourly refresh installed.', 'Dublyo', 5);
}

function removeTrigger() {
  ScriptApp.getProjectTriggers().forEach(function (t) {
    if (t.getHandlerFunction() === 'refreshAll') ScriptApp.deleteTrigger(t);
  });
}
