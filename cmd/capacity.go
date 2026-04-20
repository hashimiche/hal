package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"hal/internal/global"

	"github.com/spf13/cobra"
)

const (
	barWidth      = 44
	ansiReset     = "\033[0m"
	ansiRed       = "\033[31m"
	ansiGray      = "\033[90m"
	ansiYellow    = "\033[33m"
	ansiGreen     = "\033[32m"
	ansiBlue      = "\033[34m"
	ansiCyan      = "\033[36m"
	ansiMagenta   = "\033[35m"
	ansiSoftRed   = "\033[91m"
	ansiMauve     = "\033[95m"
	ansiOrange    = "\033[38;5;214m"
	barCellFilled = "■"
	barCellEmpty  = "□"
	barCellAdd    = "▣"
)

type stackComponent struct {
	name      string
	ratio     float64
	color     string
	colorName string
}

type stackDetail struct {
	machineCPUPercent float64
	ramMB             int
}

var (
	capacityActive   bool
	capacityDeployed bool
	capacityPending  bool
)

var capacityCmd = &cobra.Command{
	Use:   "capacity",
	Short: "Show local runtime capacity and HAL deployment estimates",
	Long:  `Displays live engine resource usage and HAL what-if estimates for heavy deployment scenarios.`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		if capacityPending && (capacityActive || capacityDeployed) {
			fmt.Println("❌ Invalid flags: use only one of --pending or --active/--deployed")
			return
		}

		view := "current"
		if capacityPending {
			view = "pending"
		} else if capacityActive || capacityDeployed {
			view = "active"
		}

		usage, err := global.GetEngineUsage(engine)
		if err != nil {
			fmt.Printf("❌ Failed to query %s capacity: %v\n", engine, err)
			return
		}

		fmt.Println("HAL Capacity Advisor")
		fmt.Println("====================")
		fmt.Printf("Engine:    %s\n", strings.Title(engine))
		fmt.Printf("Machine:   %d CPU / %s RAM (%s)\n", usage.CPUs, global.FormatMemoryMiB(usage.MemoryMB), usage.State)
		fmt.Printf("Live:      %.1f%% CPU | %s RAM pressure\n", usage.LiveCPUPercent, global.FormatMemoryMiB(usage.LiveMemMB))
		trackedMachineCPU := toMachineCPUPercent(usage.ContainerCPUPercent, usage.CPUs)
		fmt.Printf("Tracked:   %.1f%% CPU(sum) ~= %.1f%% machine | %s RAM across %d containers\n", usage.ContainerCPUPercent, trackedMachineCPU, global.FormatMemoryMiB(usage.ContainerMemMB), usage.ContainerCount)

		memBaselineMB := global.ConservativeMemoryBaselineMB(usage)
		baseMemRatio := ratio(float64(memBaselineMB), float64(usage.MemoryMB))
		baseCPURatio := ratio(usage.LiveCPUPercent, 100.0)

		type heavyScenario struct {
			product    string
			scenario   string
			label      string
			deployed   bool
			containers []string
		}

		scenarios := []heavyScenario{
			{product: "Platform", scenario: "vault-k8s", label: "KinD + VSO (Vault K8s flow)", deployed: isVaultK8sDeployed(engine), containers: []string{"kind-control-plane"}},
			{product: "Platform", scenario: "vault-jwt", label: "GitLab CI + runner (shared service)", deployed: global.IsContainerRunning(engine, "hal-gitlab"), containers: []string{"hal-gitlab", "hal-gitlab-runner"}},
			{product: "Terraform", scenario: "terraform-deploy", label: "Terraform Enterprise stack", deployed: global.IsContainerRunning(engine, "hal-tfe"), containers: []string{"hal-tfe", "hal-tfe-db", "hal-tfe-redis", "hal-tfe-minio", "hal-tfe-proxy"}},
			{product: "Terraform", scenario: "terraform-deploy", label: "Terraform Enterprise twin stack", deployed: global.IsContainerRunning(engine, "hal-tfe-bis"), containers: []string{"hal-tfe-bis", "hal-tfe-bis-proxy"}},
			{product: "Observability", scenario: "obs-deploy", label: "Observability stack", deployed: global.IsContainerRunning(engine, "hal-grafana"), containers: []string{"hal-grafana", "hal-prometheus", "hal-loki", "hal-promtail"}},
		}
		type compColor struct {
			ansi string
			name string
		}
		componentColors := map[string]compColor{
			"GitLab CI + runner (shared service)": {ansi: ansiOrange, name: "orange"},
			"KinD + VSO (Vault K8s flow)":         {ansi: ansiBlue, name: "blue"},
			"Terraform Enterprise stack":          {ansi: ansiMagenta, name: "purple"},
			"Terraform Enterprise twin stack":     {ansi: ansiMauve, name: "mauve"},
			"Observability stack":                 {ansi: ansiRed, name: "red"},
			// Product palette kept consistent with HAL visual language for future scenarios.
			"Nomad":    {ansi: ansiGreen, name: "green"},
			"Boundary": {ansi: ansiSoftRed, name: "soft-red"},
			"Consul":   {ansi: ansiMauve, name: "mauve"},
		}

		statsByContainer, _ := getStatsByContainer(engine)
		activeCPUComponents := []stackComponent{}
		activeRAMComponents := []stackComponent{}
		activeDetails := map[string]stackDetail{}
		for _, item := range scenarios {
			if !item.deployed {
				continue
			}
			cpuPercent, memMB := footprintForScenario(statsByContainer, item.containers)
			if cpuPercent == 0 && memMB == 0 {
				continue
			}
			color := componentColors[item.label]
			if color.ansi == "" {
				color = compColor{ansi: ansiCyan, name: "cyan"}
			}
			activeCPUComponents = append(activeCPUComponents, stackComponent{
				name:      item.label,
				ratio:     ratio(toMachineCPUPercent(cpuPercent, usage.CPUs), 100.0),
				color:     color.ansi,
				colorName: color.name,
			})
			activeRAMComponents = append(activeRAMComponents, stackComponent{
				name:      item.label,
				ratio:     ratio(float64(memMB), float64(usage.MemoryMB)),
				color:     color.ansi,
				colorName: color.name,
			})
			activeDetails[item.label] = stackDetail{
				machineCPUPercent: toMachineCPUPercent(cpuPercent, usage.CPUs),
				ramMB:             memMB,
			}
		}

		if view != "pending" {
			fmt.Println()
			title := "Current Capacity"
			if view == "active" {
				title = "Current Heavy deployment"
			}
			fmt.Println(title)
			fmt.Println(strings.Repeat("-", len(title)))
			fmt.Printf("CPU  %s %.1f%%\n", renderCompositionBar(baseCPURatio, activeCPUComponents), usage.LiveCPUPercent)
			fmt.Printf("RAM  %s %s / %s (%.1f%%)\n", renderCompositionBar(baseMemRatio, activeRAMComponents), global.FormatMemoryMiB(memBaselineMB), global.FormatMemoryMiB(usage.MemoryMB), baseMemRatio*100)
			fmt.Println("Legend:")
			if view == "active" {
				if len(activeCPUComponents) == 0 {
					fmt.Println("  (no active heavy deployments)")
				} else {
					for _, comp := range activeCPUComponents {
						detail := activeDetails[comp.name]
						fmt.Printf("  %s: %s■%s %s : %.1f%% machine | %s RAM\n", comp.colorName, comp.color, ansiReset, comp.name, detail.machineCPUPercent, global.FormatMemoryMiB(detail.ramMB))
					}
				}
			} else {
				for _, comp := range activeCPUComponents {
					fmt.Printf("  %s: %s■%s %s\n", comp.colorName, comp.color, ansiReset, comp.name)
				}
				fmt.Printf("  gray: %s■%s Other\n", ansiGray, ansiReset)
			}
		}

		productOrder := []string{"Platform", "Terraform", "Observability"}
		productHasContent := map[string]bool{}

		if view == "pending" {
			fmt.Println()
			fmt.Println("Pending Heavy Deployments")
			fmt.Println("-------------------------")
			for _, product := range productOrder {
				for _, item := range scenarios {
					if item.product != product || item.deployed {
						continue
					}
					if !productHasContent[product] {
						fmt.Printf("\n%s\n", product)
						productHasContent[product] = true
					}

					estimate, err := global.EstimateEngineScenario(engine, item.scenario)
					if err != nil {
						fmt.Printf("  - %s: estimate unavailable (%v)\n", item.label, err)
						continue
					}

					severity := "🟢 Headroom healthy"
					if estimate.Severity != "" {
						if strings.Contains(strings.ToLower(estimate.Severity), "warning") {
							severity = "🟠 Likely unstable"
						} else {
							severity = "🟡 Headroom tight"
						}
					}

					fmt.Printf("  - %s -> %.1f%% CPU | %s RAM (%s)\n", item.label, estimate.EstimatedCPUPercent, global.FormatMemoryMiB(estimate.EstimatedMemoryMB), severity)
					cpuAddRatio := ratio(estimate.Profile.CPUCores, float64(usage.CPUs))
					memAddRatio := ratio(float64(estimate.Profile.MemoryMB), float64(usage.MemoryMB))
					accentColor := componentColors[item.label].ansi
					if accentColor == "" {
						accentColor = ansiBlue
					}
					fmt.Printf("    CPU impact  %s\n", renderStackedBar(baseCPURatio, cpuAddRatio, accentColor))
					fmt.Printf("    RAM impact  %s\n", renderStackedBar(baseMemRatio, memAddRatio, accentColor))
				}
			}

			if len(productHasContent) == 0 {
				fmt.Println("✅ No pending heavy deployment to estimate.")
			}
		}
	},
}

func renderSingleBar(ratio float64) string {
	filled := int(math.Round(clamp01(ratio) * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	color := severityColor(ratio)
	return fmt.Sprintf("[%s%s%s%s]", color, strings.Repeat(barCellFilled, filled), ansiReset, strings.Repeat(barCellEmpty, barWidth-filled))
}

func renderCompositionBar(usedRatio float64, components []stackComponent) string {
	usedCells := int(math.Ceil(clamp01(usedRatio) * float64(barWidth)))
	if usedRatio > 0 && usedCells < 2 {
		usedCells = 2
	}
	if usedCells < 0 {
		usedCells = 0
	}
	if usedCells > barWidth {
		usedCells = barWidth
	}

	totalRatio := 0.0
	for _, comp := range components {
		if comp.ratio > 0 {
			totalRatio += comp.ratio
		}
	}

	builder := "["
	remaining := usedCells
	for i, comp := range components {
		if comp.ratio <= 0 || remaining <= 0 || totalRatio <= 0 {
			continue
		}
		cells := int(math.Round((comp.ratio / totalRatio) * float64(usedCells)))
		if cells < 0 {
			cells = 0
		}
		if i == len(components)-1 || cells > remaining {
			cells = remaining
		}
		if cells > 0 {
			builder += comp.color + strings.Repeat(barCellFilled, cells) + ansiReset
			remaining -= cells
		}
	}
	if remaining > 0 {
		builder += ansiGray + strings.Repeat(barCellFilled, remaining) + ansiReset
	}
	builder += strings.Repeat(barCellEmpty, barWidth-usedCells) + "]"
	return builder
}

func renderStackedBar(baseRatio, addRatio float64, accentColor string) string {
	base := clamp01(baseRatio)
	add := math.Max(0, addRatio)
	total := base + add
	if accentColor == "" {
		accentColor = ansiBlue
	}

	baseCells := int(math.Round(base * float64(barWidth)))
	if baseCells > barWidth {
		baseCells = barWidth
	}
	if baseCells < 0 {
		baseCells = 0
	}

	addCells := int(math.Round(add * float64(barWidth)))
	if addCells < 0 {
		addCells = 0
	}
	if baseCells+addCells > barWidth {
		addCells = barWidth - baseCells
	}
	if addCells < 0 {
		addCells = 0
	}

	remaining := barWidth - baseCells - addCells
	if remaining < 0 {
		remaining = 0
	}

	overflow := ""
	if total > 1.0 {
		overflowRatio := total - 1.0
		overflowCells := int(math.Round(overflowRatio * float64(barWidth)))
		if overflowCells < 1 {
			overflowCells = 1
		}
		if overflowCells > barWidth {
			overflowCells = barWidth
		}
		overflow = fmt.Sprintf("%s%s%s %s+%.1f%%%s", accentColor, strings.Repeat(barCellAdd, overflowCells), ansiReset, accentColor, overflowRatio*100.0, ansiReset)
	}

	return fmt.Sprintf("[%s%s%s%s%s%s%s]%s",
		ansiGray, strings.Repeat(barCellFilled, baseCells), ansiReset,
		accentColor, strings.Repeat(barCellAdd, addCells), ansiReset,
		strings.Repeat(barCellEmpty, remaining), overflow,
	)
}

func severityColor(ratio float64) string {
	switch {
	case ratio >= 0.90:
		return ansiRed
	case ratio >= 0.75:
		return ansiYellow
	default:
		return ansiGreen
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func toMachineCPUPercent(containerCPUSum float64, cpus int) float64 {
	if cpus <= 0 {
		return 0
	}
	return containerCPUSum / float64(cpus)
}

type containerStat struct {
	Name       string
	CPUPercent string
	MemUsage   string
}

func getStatsByContainer(engine string) (map[string]containerStat, error) {
	out, err := exec.Command(engine, "stats", "--no-stream", "--format", "json").Output()
	if err != nil {
		return nil, err
	}

	stats, err := parseStatsOutput(string(out))
	if err != nil {
		return nil, err
	}

	indexed := map[string]containerStat{}
	for _, stat := range stats {
		indexed[stat.Name] = stat
	}
	return indexed, nil
}

func parseStatsOutput(raw string) ([]containerStat, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []containerStat{}, nil
	}
	if strings.HasPrefix(raw, "[") {
		type statsJSON struct {
			Name       string `json:"name"`
			CPUPercent string `json:"cpu_percent"`
			MemUsage   string `json:"mem_usage"`
		}
		decoded := []statsJSON{}
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return nil, err
		}
		stats := make([]containerStat, 0, len(decoded))
		for _, stat := range decoded {
			stats = append(stats, containerStat{Name: stat.Name, CPUPercent: stat.CPUPercent, MemUsage: stat.MemUsage})
		}
		return stats, nil
	}
	lines := strings.Split(raw, "\n")
	stats := make([]containerStat, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name := extractJSONField(line, "name")
		cpu := extractJSONField(line, "cpu_percent")
		mem := extractJSONField(line, "mem_usage")
		if name == "" && cpu == "" && mem == "" {
			continue
		}
		stats = append(stats, containerStat{Name: name, CPUPercent: cpu, MemUsage: mem})
	}
	return stats, nil
}

func extractJSONField(line, key string) string {
	prefix := fmt.Sprintf("\"%s\":", key)
	idx := strings.Index(line, prefix)
	if idx == -1 {
		return ""
	}
	remainder := strings.TrimSpace(line[idx+len(prefix):])
	if !strings.HasPrefix(remainder, "\"") {
		return ""
	}
	remainder = remainder[1:]
	end := strings.Index(remainder, "\"")
	if end == -1 {
		return ""
	}
	return remainder[:end]
}

func footprintForScenario(statsByContainer map[string]containerStat, containers []string) (float64, int) {
	totalCPU := 0.0
	totalMemMB := 0
	for _, container := range containers {
		stat, ok := statsByContainer[container]
		if !ok {
			continue
		}
		totalCPU += parsePercent(stat.CPUPercent)
		totalMemMB += parseMemoryUsageMB(stat.MemUsage)
	}
	return totalCPU, totalMemMB
}

func parsePercent(raw string) float64 {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	v, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseMemoryUsageMB(raw string) int {
	parts := strings.Split(raw, "/")
	if len(parts) == 0 {
		return 0
	}
	return parseMemoryValueMB(parts[0])
}

func parseMemoryValueMB(raw string) int {
	trimmed := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	trimmed = strings.ReplaceAll(trimmed, "iB", "B")
	for _, unit := range []string{"TB", "GB", "MB", "KB", "B"} {
		if strings.HasSuffix(trimmed, unit) {
			number := strings.TrimSuffix(trimmed, unit)
			value, err := strconv.ParseFloat(number, 64)
			if err != nil {
				return 0
			}
			switch unit {
			case "TB":
				return int(math.Round(value * 1024 * 1024))
			case "GB":
				return int(math.Round(value * 1024))
			case "MB":
				return int(math.Round(value))
			case "KB":
				return int(math.Round(value / 1024))
			default:
				return 0
			}
		}
	}
	return 0
}

func isVaultK8sDeployed(engine string) bool {
	if global.IsContainerRunning(engine, "kind-control-plane") {
		return true
	}
	out, err := exec.Command("kind", "get", "clusters").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "kind")
}

func init() {
	capacityCmd.Flags().BoolVar(&capacityActive, "active", false, "Show active heavy deployment details")
	capacityCmd.Flags().BoolVar(&capacityDeployed, "deployed", false, "Alias for --active")
	capacityCmd.Flags().BoolVar(&capacityPending, "pending", false, "Show pending heavy deployment impact estimates")
	rootCmd.AddCommand(capacityCmd)
}
