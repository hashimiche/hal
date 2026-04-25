package halhealth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

// Cmd is the root cobra command for `hal health`.
var Cmd = &cobra.Command{
	Use:   "health",
	Short: "Manage the hal-status runtime container",
	Long:  `Create, update, or delete the hal-status container that serves live ecosystem state to hal-plus and other consumers.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create (or recreate) the hal-status container",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would create %s container on hal-net (port %d)\n",
				global.HalStatusContainerName, global.HalStatusPort)
			return
		}
		global.EnsureNetwork(engine)
		global.RefreshHalStatus(engine)
		fmt.Println("✅ hal-status container created.")
		fmt.Printf("   API: http://hal-status:%d/api/status (from hal-net)\n", global.HalStatusPort)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Refresh the hal-status snapshot (re-inspect the ecosystem and replace the container)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would refresh %s snapshot\n", global.HalStatusContainerName)
			return
		}
		global.RefreshHalStatus(engine)
		fmt.Println("✅ hal-status snapshot refreshed.")
	},
}

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Remove the hal-status container",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}
		if global.DryRun {
			fmt.Printf("[DRY RUN] Would remove container %s\n", global.HalStatusContainerName)
			return
		}
		global.RemoveHalStatus(engine)
		fmt.Printf("✅ %s removed.\n", global.HalStatusContainerName)
	},
}

// serveCmd is hidden — called by the container entrypoint, not the user.
// It reads HAL_STATUS_DATA and serves a minimal JSON + HTML status API.
var serveCmd = &cobra.Command{
	Use:    "_serve",
	Short:  "Internal: serve HAL_STATUS_DATA over HTTP (used by the hal-status container)",
	Hidden: true,
	Args:   cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		raw := os.Getenv("HAL_STATUS_DATA")
		if raw == "" {
			fmt.Fprintln(os.Stderr, "HAL_STATUS_DATA not set")
			os.Exit(1)
		}

		var snap global.StatusSnapshot
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse HAL_STATUS_DATA: %v\n", err)
			os.Exit(1)
		}

		port := global.HalStatusPort
		if v := os.Getenv("HAL_STATUS_PORT"); v != "" {
			if p, err := strconv.Atoi(v); err == nil {
				port = p
			}
		}

		mux := http.NewServeMux()

		// GET /api/status — full snapshot
		mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			_ = enc.Encode(snap)
		})

		// GET /api/products — products array
		mux.HandleFunc("/api/products", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			_ = enc.Encode(snap.Products)
		})

		// GET /api/products/{name} — single product
		mux.HandleFunc("/api/products/", func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(r.URL.Path, "/api/products/")
			name = strings.TrimSuffix(name, "/")
			if name == "" {
				http.Redirect(w, r, "/api/products", http.StatusMovedPermanently)
				return
			}
			for _, p := range snap.Products {
				if strings.EqualFold(p.Product, name) {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("Access-Control-Allow-Origin", "*")
					enc := json.NewEncoder(w)
					enc.SetIndent("", "  ")
					_ = enc.Encode(p)
					return
				}
			}
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
		})

		// GET / — simple HTML status page
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, htmlStatusPage(snap))
		})

		addr := fmt.Sprintf(":%d", port)
		fmt.Printf("hal-status serving on %s (snapshot: %s)\n", addr, snap.Timestamp)
		srv := &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	},
}

func htmlStatusPage(snap global.StatusSnapshot) string {
	var rows strings.Builder
	for _, p := range snap.Products {
		stateColor := "#e74c3c"
		if p.State == "running" {
			stateColor = "#27ae60"
		} else if p.State == "partial" {
			stateColor = "#f39c12"
		}
		var featureCells strings.Builder
		for _, f := range p.Features {
			fc := "#aaa"
			if f.State == "enabled" {
				fc = "#27ae60"
			}
			featureCells.WriteString(fmt.Sprintf(
				`<span style="margin-right:8px;color:%s">&#9679; %s</span>`, fc, f.Feature))
		}
		rows.WriteString(fmt.Sprintf(`
<tr>
  <td style="padding:8px 12px;font-weight:600">%s</td>
  <td style="padding:8px 12px;color:%s">%s</td>
  <td style="padding:8px 12px"><a href="%s" target="_blank">%s</a></td>
  <td style="padding:8px 12px;font-size:0.85em">%s</td>
</tr>`, p.Product, stateColor, p.State, p.Endpoint, p.Endpoint, featureCells.String()))
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>HAL Status</title>
<style>body{font-family:monospace;background:#111;color:#eee;padding:24px}
table{border-collapse:collapse;width:100%%}
th{text-align:left;padding:8px 12px;border-bottom:1px solid #333;color:#aaa;font-size:0.8em;text-transform:uppercase}
tr:hover{background:#1a1a1a}a{color:#7c3aed}</style>
</head>
<body>
<h2 style="color:#7c3aed">HAL Ecosystem Status</h2>
<p style="color:#aaa;font-size:0.85em">Snapshot: %s &nbsp;·&nbsp; Engine: %s</p>
<table>
<thead><tr><th>Product</th><th>State</th><th>Endpoint</th><th>Features</th></tr></thead>
<tbody>%s</tbody>
</table>
<p style="color:#555;font-size:0.75em;margin-top:24px">API: <a href="/api/status">/api/status</a> &nbsp;·&nbsp; <a href="/api/products">/api/products</a></p>
</body></html>`, snap.Timestamp, snap.Engine, rows.String())
}

func init() {
	Cmd.AddCommand(createCmd, updateCmd, deleteCmd, serveCmd)
}
