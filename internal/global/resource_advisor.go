package global

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type EngineUsage struct {
	Engine              string
	CPUs                int
	MemoryMB            int
	LiveCPUPercent      float64
	LiveMemMB           int
	LiveMemRawMB        int
	LiveMemAllocMB      int
	ContainerCount      int
	ContainerCPUPercent float64
	ContainerMemMB      int
	LiveSource          string
	State               string
}

type ScenarioProfile struct {
	Name     string
	CPUCores float64
	MemoryMB int
}

type ScenarioEstimate struct {
	Scenario            string
	Profile             ScenarioProfile
	MachineCPUs         int
	MachineMemoryMB     int
	LiveCPUPercent      float64
	LiveMemoryMB        int
	ContainerCount      int
	EstimatedCPUPercent float64
	EstimatedMemoryMB   int
	Severity            string
	Suggestion          string
}

type podmanMachineInspect struct {
	Resources struct {
		CPUs   int `json:"CPUs"`
		Memory int `json:"Memory"`
	} `json:"Resources"`
	State string `json:"State"`
}

type containerStat struct {
	Name       string `json:"name"`
	CPUPercent string `json:"cpu_percent"`
	MemUsage   string `json:"mem_usage"`
}

type dockerInfo struct {
	NCPU          int    `json:"NCPU"`
	MemTotal      int64  `json:"MemTotal"`
	ServerVersion string `json:"ServerVersion"`
}

var scenarioProfiles = map[string]ScenarioProfile{
	"vault-k8s":        {Name: "Vault K8s (KinD + VSO)", CPUCores: 2.0, MemoryMB: 3072},
	"vault-jwt":        {Name: "Vault JWT (GitLab + runner)", CPUCores: 2.5, MemoryMB: 6144},
	"terraform-deploy": {Name: "Terraform Enterprise stack", CPUCores: 4.0, MemoryMB: 4608},
	"obs-deploy":       {Name: "Observability stack", CPUCores: 1.0, MemoryMB: 1536},
}

func GetEngineUsage(engine string) (*EngineUsage, error) {
	switch engine {
	case "podman":
		return getPodmanMachineUsage()
	case "docker":
		return getDockerUsage()
	default:
		return nil, fmt.Errorf("resource advisor does not support engine: %s", engine)
	}
}

func getPodmanMachineUsage() (*EngineUsage, error) {
	out, err := exec.Command("podman", "machine", "inspect", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, err
	}

	var machine podmanMachineInspect
	if err := json.Unmarshal(out, &machine); err != nil {
		return nil, err
	}

	stats, err := getContainerStats("podman")
	if err != nil {
		return nil, err
	}

	usage := &EngineUsage{
		Engine:         "podman",
		CPUs:           machine.Resources.CPUs,
		MemoryMB:       machine.Resources.Memory,
		ContainerCount: len(stats),
		LiveSource:     "container-aggregate",
		State:          machine.State,
	}

	for _, stat := range stats {
		usage.ContainerCPUPercent += parsePercent(stat.CPUPercent)
		usage.ContainerMemMB += parseMemoryUsageMB(stat.MemUsage)
	}

	usage.LiveCPUPercent = usage.ContainerCPUPercent
	usage.LiveMemMB = usage.ContainerMemMB
	usage.LiveMemRawMB = usage.ContainerMemMB
	usage.LiveMemAllocMB = usage.ContainerMemMB

	if engineCPUPercent, engineMemMB, engineMemRawMB, engineMemAllocMB, runtimeErr := getPodmanMachineRuntimeUsage(); runtimeErr == nil {
		usage.LiveCPUPercent = engineCPUPercent
		usage.LiveMemMB = engineMemMB
		usage.LiveMemRawMB = engineMemRawMB
		usage.LiveMemAllocMB = engineMemAllocMB
		usage.LiveSource = "machine-runtime"
	}

	return usage, nil
}

func getDockerUsage() (*EngineUsage, error) {
	out, err := exec.Command("docker", "info", "--format", "{{json .}}").Output()
	if err != nil {
		return nil, err
	}

	var info dockerInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}

	stats, err := getContainerStats("docker")
	if err != nil {
		return nil, err
	}

	usage := &EngineUsage{
		Engine:         "docker",
		CPUs:           info.NCPU,
		MemoryMB:       int(info.MemTotal / (1024 * 1024)),
		ContainerCount: len(stats),
		LiveSource:     "container-aggregate",
		State:          "running",
	}

	for _, stat := range stats {
		usage.ContainerCPUPercent += parsePercent(stat.CPUPercent)
		usage.ContainerMemMB += parseMemoryUsageMB(stat.MemUsage)
	}

	usage.LiveCPUPercent = usage.ContainerCPUPercent
	usage.LiveMemMB = usage.ContainerMemMB
	usage.LiveMemRawMB = usage.ContainerMemMB
	usage.LiveMemAllocMB = usage.ContainerMemMB

	return usage, nil
}

func GetScenarioProfile(name string) (ScenarioProfile, bool) {
	profile, ok := scenarioProfiles[name]
	return profile, ok
}

func WarnIfEngineResourcesTight(engine, scenario string) {
	profile, ok := GetScenarioProfile(scenario)
	if !ok {
		return
	}

	usage, err := GetEngineUsage(engine)
	if err != nil {
		if Debug {
			fmt.Printf("[DEBUG] Engine resource advisory unavailable: %v\n", err)
		}
		return
	}

	baseMemMB := conservativeMemoryBaselineMB(usage)
	plannedMemMB := baseMemMB + profile.MemoryMB
	liveCPUCores := (usage.LiveCPUPercent / 100.0) * float64(usage.CPUs)
	plannedCPUCores := liveCPUCores + profile.CPUCores

	memRatio := ratio(float64(plannedMemMB), float64(usage.MemoryMB))
	cpuRatio := ratio(plannedCPUCores, float64(usage.CPUs))

	severity, suggestion := engineSeverity(engine, memRatio, cpuRatio)
	if severity == "" {
		return
	}

	fmt.Println(severity)
	fmt.Printf("   %s -> estimated after deploy: %.1f%% CPU / %s RAM on %s (%d CPU / %s RAM)\n", profile.Name, cpuRatio*100.0, formatMiB(plannedMemMB), strings.Title(engine), usage.CPUs, formatMiB(usage.MemoryMB))
	fmt.Printf("   %s\n", suggestion)
}

func ConfirmScenarioProceed(engine, scenario string) (bool, error) {
	estimate, err := EstimateEngineScenario(engine, scenario)
	if err != nil {
		return true, err
	}

	if !scenarioExceedsEngineLimit(estimate) {
		return true, nil
	}

	engineName := engine
	if engineName == "" {
		engineName = "container"
	}

	fmt.Printf("⚠️  %s would exceed %s capacity.\n", estimate.Profile.Name, strings.Title(engineName))
	fmt.Printf("   Estimated after deploy: %.1f%% CPU / %s RAM\n", estimate.EstimatedCPUPercent, formatMiB(estimate.EstimatedMemoryMB))
	fmt.Printf("   This can kill or destabilize your %s engine.\n", engineName)
	fmt.Print("   Do you want to proceed? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.ToLower(strings.TrimSpace(response))

	return response == "y" || response == "yes", nil
}

func EstimateEngineScenario(engine, scenario string) (*ScenarioEstimate, error) {
	profile, ok := GetScenarioProfile(scenario)
	if !ok {
		return nil, fmt.Errorf("unknown scenario: %s", scenario)
	}

	usage, err := GetEngineUsage(engine)
	if err != nil {
		return nil, err
	}

	baseMemMB := conservativeMemoryBaselineMB(usage)
	plannedMemMB := baseMemMB + profile.MemoryMB
	liveCPUCores := (usage.LiveCPUPercent / 100.0) * float64(usage.CPUs)
	plannedCPUCores := liveCPUCores + profile.CPUCores
	cpuRatio := ratio(plannedCPUCores, float64(usage.CPUs))
	memRatio := ratio(float64(plannedMemMB), float64(usage.MemoryMB))
	severity, suggestion := engineSeverity(engine, memRatio, cpuRatio)

	return &ScenarioEstimate{
		Scenario:            scenario,
		Profile:             profile,
		MachineCPUs:         usage.CPUs,
		MachineMemoryMB:     usage.MemoryMB,
		LiveCPUPercent:      usage.LiveCPUPercent,
		LiveMemoryMB:        usage.LiveMemMB,
		ContainerCount:      usage.ContainerCount,
		EstimatedCPUPercent: cpuRatio * 100.0,
		EstimatedMemoryMB:   plannedMemMB,
		Severity:            severity,
		Suggestion:          suggestion,
	}, nil
}

func FormatMemoryMiB(memoryMB int) string {
	return formatMiB(memoryMB)
}

func engineSeverity(engine string, memRatio, cpuRatio float64) (string, string) {
	engineTitle := strings.Title(engine)
	switch {
	case memRatio > 1.0 || cpuRatio > 1.0:
		return fmt.Sprintf("⚠️  %s capacity warning", engineTitle), "This deploy is likely to fail or become unstable without freeing resources first."
	case memRatio >= 0.90 || cpuRatio >= 0.90:
		return fmt.Sprintf("ℹ️  %s capacity note", engineTitle), "This deploy should still be possible, but headroom is getting tight."
	default:
		return "", ""
	}
}

func scenarioExceedsEngineLimit(estimate *ScenarioEstimate) bool {
	if estimate == nil {
		return false
	}
	if estimate.MachineCPUs > 0 && estimate.EstimatedCPUPercent > 100.0 {
		return true
	}
	return estimate.MachineMemoryMB > 0 && estimate.EstimatedMemoryMB > estimate.MachineMemoryMB
}

func getContainerStats(engine string) ([]containerStat, error) {
	out, err := exec.Command(engine, "stats", "--no-stream", "--format", "json").Output()
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return []containerStat{}, nil
	}

	var stats []containerStat
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(out, &stats); err != nil {
			return nil, err
		}
		return stats, nil
	}

	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var stat containerStat
		if err := json.Unmarshal([]byte(line), &stat); err != nil {
			return nil, err
		}
		stats = append(stats, stat)
	}
	return stats, nil
}

func parsePercent(raw string) float64 {
	trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return value
}

func parseMemoryUsageMB(raw string) int {
	parts := strings.Split(raw, "/")
	if len(parts) == 0 {
		return 0
	}
	return parseMemoryValueMB(parts[0])
}

func parseMemoryValueMB(raw string) int {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.ReplaceAll(trimmed, "iB", "B")
	trimmed = strings.ReplaceAll(trimmed, " ", "")

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

func formatMiB(memoryMB int) string {
	if memoryMB >= 1024 {
		return fmt.Sprintf("%.2f GiB", float64(memoryMB)/1024.0)
	}
	return fmt.Sprintf("%d MiB", memoryMB)
}

func ratio(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	return numerator / denominator
}

func getPodmanMachineRuntimeUsage() (float64, int, int, int, error) {
	memOut, err := exec.Command("podman", "machine", "ssh", "--", "free", "-m").Output()
	if err != nil {
		return 0, 0, 0, 0, err
	}

	memPressureMB, memRawMB, memAllocMB, err := parsePodmanMachineMemUsedMB(string(memOut))
	if err != nil {
		return 0, 0, 0, 0, err
	}

	cpuOut, err := exec.Command("podman", "machine", "ssh", "--", "top", "-bn1").Output()
	if err != nil {
		return 0, memPressureMB, memRawMB, memAllocMB, nil
	}

	cpuUsed := parsePodmanMachineCPUPercent(string(cpuOut))
	if cpuUsed < 0 {
		return 0, memPressureMB, memRawMB, memAllocMB, nil
	}

	return cpuUsed, memPressureMB, memRawMB, memAllocMB, nil
}

func parsePodmanMachineMemUsedMB(freeOutput string) (int, int, int, error) {
	for _, line := range strings.Split(freeOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Mem:") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 7 {
			break
		}
		total, errTotal := strconv.Atoi(fields[1])
		rawUsed, errRaw := strconv.Atoi(fields[2])
		freeMem, errFree := strconv.Atoi(fields[3])
		available, errAvail := strconv.Atoi(fields[6])
		if errTotal != nil || errAvail != nil || errRaw != nil || errFree != nil {
			break
		}
		used := total - available
		allocated := total - freeMem
		if used < 0 {
			used = 0
		}
		if allocated < 0 {
			allocated = 0
		}
		return used, rawUsed, allocated, nil
	}
	return 0, 0, 0, fmt.Errorf("unable to parse podman machine memory usage")
}

func ConservativeMemoryBaselineMB(usage *EngineUsage) int {
	return conservativeMemoryBaselineMB(usage)
}

func conservativeMemoryBaselineMB(usage *EngineUsage) int {
	// Use pressure-based memory (total - available), which excludes reclaimable cache/buffers.
	// Keep container sum as a floor to avoid under-counting when engine runtime stats lag.
	baseline := usage.LiveMemMB
	if usage.ContainerMemMB > baseline {
		baseline = usage.ContainerMemMB
	}
	return baseline
}

func parsePodmanMachineCPUPercent(topOutput string) float64 {
	lines := strings.Split(topOutput, "\n")
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "cpu") {
			continue
		}
		normalized := strings.ReplaceAll(strings.ReplaceAll(line, ",", " "), "%", "")
		fields := strings.Fields(normalized)
		for i := 1; i < len(fields); i++ {
			if strings.Contains(strings.ToLower(fields[i]), "id") {
				idle, err := strconv.ParseFloat(strings.TrimSpace(fields[i-1]), 64)
				if err == nil {
					cpuUsed := 100.0 - idle
					if cpuUsed < 0 {
						cpuUsed = 0
					}
					return cpuUsed
				}
			}
		}
	}
	return -1
}
