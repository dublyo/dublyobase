package core

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func enableDefaultDenyRLS(ctx context.Context, tx pgx.Tx, schemaName string, tableName string) error {
	table := quoteIdent(schemaName, tableName)
	policies := []string{
		fmt.Sprintf(`alter table %s enable row level security`, table),
		fmt.Sprintf(`alter table %s force row level security`, table),
		fmt.Sprintf(`drop policy if exists %s on %s`, quoteIdent(tableName+"_select_deny"), table),
		fmt.Sprintf(`drop policy if exists %s on %s`, quoteIdent(tableName+"_insert_deny"), table),
		fmt.Sprintf(`drop policy if exists %s on %s`, quoteIdent(tableName+"_update_deny"), table),
		fmt.Sprintf(`drop policy if exists %s on %s`, quoteIdent(tableName+"_delete_deny"), table),
		fmt.Sprintf(`create policy %s on %s for select using (false)`, quoteIdent(tableName+"_select_deny"), table),
		fmt.Sprintf(`create policy %s on %s for insert with check (false)`, quoteIdent(tableName+"_insert_deny"), table),
		fmt.Sprintf(`create policy %s on %s for update using (false) with check (false)`, quoteIdent(tableName+"_update_deny"), table),
		fmt.Sprintf(`create policy %s on %s for delete using (false)`, quoteIdent(tableName+"_delete_deny"), table),
	}
	for _, stmt := range policies {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
