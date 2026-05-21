package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/mvanhorn/printing-press-library/library/trading/mt5/internal/store"
)

// `pp-mt5 audit ...` reads the audit log. The file at <store_dir>/audit.jsonl
// is the source of truth; the audit DB table is a queryable mirror.

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "audit", Short: "Read the write-command audit log"}

	var limit int
	tail := &cobra.Command{
		Use:   "tail",
		Short: "Show the most recent N audit entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := store.OpenAndMigrate("")
			if err != nil {
				return &ExitErr{Code: ExitConfig, Err: err}
			}
			defer db.Close()
			rows, err := db.QueryContext(cmd.Context(), `
				SELECT time_ms, command, hash, confirmed, mode, account_login,
				       COALESCE(error,''), COALESCE(response,'')
				FROM audit ORDER BY time_ms DESC LIMIT ?`, limit)
			if err != nil {
				return err
			}
			defer rows.Close()
			type entry struct {
				TimeMS, AccountLogin int64
				Command, Hash, Mode  string
				Confirmed            int
				Error, Response      string
			}
			var es []entry
			for rows.Next() {
				var e entry
				if err := rows.Scan(&e.TimeMS, &e.Command, &e.Hash, &e.Confirmed, &e.Mode,
					&e.AccountLogin, &e.Error, &e.Response); err != nil {
					return err
				}
				es = append(es, e)
			}
			return emit(cmd, es, func(w io.Writer, v any) {
				items := v.([]entry)
				if len(items) == 0 {
					fmt.Fprintln(w, "audit log empty — no write commands have run yet")
					return
				}
				tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
				fmt.Fprintln(tw, "TIME\tCOMMAND\tMODE\tCONF\tACCT\tRESULT")
				fmt.Fprintln(tw, "────\t───────\t────\t────\t────\t──────")
				for _, e := range items {
					result := truncate(e.Response, 60)
					if e.Error != "" {
						result = "ERR: " + truncate(e.Error, 56)
					}
					conf := "—"
					if e.Confirmed == 1 {
						conf = "✓"
					}
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
						time.UnixMilli(e.TimeMS).Format("2006-01-02 15:04:05"),
						e.Command, e.Mode, conf, e.AccountLogin, result)
				}
				tw.Flush()
				fmt.Fprintf(w, "\n(%d entr%s; jsonl at %s)\n", len(items), iesIfPlural(len(items)), store.AuditPath())
			})
		},
	}
	tail.Flags().IntVar(&limit, "limit", 50, "Number of entries to show")
	cmd.AddCommand(tail)

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the path to audit.jsonl",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), store.AuditPath())
		},
	})

	return cmd
}

func iesIfPlural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// removeIgnore deletes the file, swallowing not-exist errors. Used by
// config-init --force.
func removeIgnore(p string) error {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
