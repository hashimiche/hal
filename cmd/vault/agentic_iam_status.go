package vault

// agentic_iam_status.go is `hal vault agentic-iam status`, the default action.
// It only reads: the containers and the image, Vault (prerequisites, profile,
// Agent Registry record, agentic-db/), Authentik (the three applications) and
// the shared MariaDB's consumers.

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"hal/internal/agenticiam"
	"hal/internal/global"
	"hal/internal/integrations"
)

// agenticIAMContainerInfo is a container as inspect reports it.
type agenticIAMContainerInfo struct {
	Exists bool
	Status string // running, exited, created, ...
	Image  string
}

func inspectAgenticIAMContainer(engine, name string) agenticIAMContainerInfo {
	out, err := exec.Command(engine, "inspect", "-f", "{{.State.Status}}|{{.Config.Image}}", name).Output()
	if err != nil {
		return agenticIAMContainerInfo{}
	}
	status, image, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	return agenticIAMContainerInfo{Exists: true, Status: status, Image: image}
}

func statusIcon(ok bool) string {
	if ok {
		return "✅"
	}
	return "⚪"
}

func runAgenticIAMStatus(lab *agenticIAMLab) {
	fmt.Println("🔍 Vault Agentic IAM lab status")
	fmt.Println()

	image := agenticiam.ImageRef()
	chat := inspectAgenticIAMContainer(lab.engine, agenticIAMChatContainer)
	agent := inspectAgenticIAMContainer(lab.engine, agenticIAMAgentContainer)
	fmt.Println("  [ Containers ]")
	printAgenticIAMContainerLine("Chat (BFF)", agenticIAMChatContainer, chat, agenticIAMChatPublicURL, image)
	printAgenticIAMContainerLine("Demo agent", agenticIAMAgentContainer, agent, "hal-net only", image)
	if exec.Command(lab.engine, "image", "inspect", image).Run() == nil {
		fmt.Printf("  ✅ %-16s : %s\n", "Image", image)
	} else {
		fmt.Printf("  ⚪ %-16s : %s (not built yet: enable builds it)\n", "Image", image)
	}

	fmt.Println()
	fmt.Println("  [ Vault ]")
	var prereqErr error
	if lab.vaultErr != nil || lab.client == nil {
		fmt.Printf("  ⚪ %-16s : %v\n", "Vault", lab.vaultErr)
	} else {
		prereqErr = checkAgenticIAMPrerequisites(lab.client, lab.prod)
		if prereqErr == nil {
			fmt.Printf("  ✅ %-16s : Vault Enterprise %s+, %s/, Agentic IAM licensed\n", "Prerequisites", agenticIAMMinVaultVersion, agentRegistryMount)
		} else {
			first, _, _ := strings.Cut(prereqErr.Error(), "\n")
			fmt.Printf("  ❌ %-16s : %s\n", "Prerequisites", first)
		}
		objects := agenticIAMVaultObjects(lab.client)
		has := func(prefix string) bool {
			return slices.ContainsFunc(objects, func(o string) bool { return strings.HasPrefix(o, prefix) })
		}
		fmt.Printf("  %s %-16s : %s (OAuth resource server profile)\n", statusIcon(has("profile ")), "Profile", agenticIAMProfileName)
		fmt.Printf("  %s %-16s : %s, ceiling %s\n", statusIcon(has("Agent Registry record ")), "Agent Registry", agenticIAMAgentName, agenticIAMCeilingPolicy)
		fmt.Printf("  %s %-16s : %s/ (roles %s, %s, %s)\n", statusIcon(has(agenticDBMount+"/")), "Database mount", agenticDBMount,
			agenticDBQuarterlyResults.Name, agenticDBForecasts.Name, agenticDBPayroll.Name)
	}

	fmt.Println()
	fmt.Println("  [ Authentik ]")
	authentikUp := integrations.IsAuthentikRunning(lab.engine)
	authentikConsumers := global.GetSharedServiceConsumers(integrations.AuthentikSharedServiceKey)
	if !authentikUp {
		fmt.Printf("  ⚪ %-16s : not running (enable starts it)\n", "Authentik")
	} else {
		version := runningAuthentikVersion(lab.engine)
		versionOK := version != "" && vaultVersionAtLeast(version, agenticIAMMinAuthentikVersion)
		icon := statusIcon(versionOK)
		if version != "" && !versionOK {
			icon = "❌"
		}
		if version == "" {
			version = "unknown"
		}
		fmt.Printf("  %s %-16s : %s (the lab needs %s+), %s\n", icon, "Authentik", version, agenticIAMMinAuthentikVersion, integrations.AuthentikAdminURL())
		apps, err := agenticIAMAuthentikApps()
		for _, slug := range []string{integrations.AgenticIAMChatSlug, integrations.AgenticIAMDemoAgentSlug, integrations.AgenticIAMVaultSlug} {
			fmt.Printf("  %s %-16s : %s\n", statusIcon(slices.Contains(apps, slug)), "Application", slug)
		}
		if err != nil {
			fmt.Printf("  ⚠️  Could not query Authentik's applications: %v\n", err)
		}
	}
	fmt.Printf("  %s %-16s : %s\n", statusIcon(slices.Contains(authentikConsumers, integrations.AgenticIAMAuthentikConsumer)),
		"Consumers", listOrNone(authentikConsumers))

	fmt.Println()
	fmt.Println("  [ MariaDB ]")
	mariadb := inspectAgenticIAMContainer(lab.engine, vaultMariaDBContainer)
	mariadbState := "not running (enable starts it)"
	if mariadb.Exists {
		mariadbState = mariadb.Status
	}
	fmt.Printf("  %s %-16s : %s\n", statusIcon(mariadb.Status == "running"), vaultMariaDBContainer, mariadbState)
	mariadbConsumers := global.GetSharedServiceConsumers(global.SharedVaultMariaDBServiceKey)
	fmt.Printf("  %s %-16s : %s\n", statusIcon(slices.Contains(mariadbConsumers, global.VaultMariaDBConsumerAgenticIAM)),
		"Consumers", listOrNone(mariadbConsumers))

	fmt.Println()
	fmt.Printf("  Chat URL : %s\n", agenticIAMChatPublicURL)
	fmt.Println()
	if authentikUp {
		printAuthentikAdminReference()
	}

	fmt.Println("💡 Next step:")
	running := chat.Status == "running" && agent.Status == "running"
	enabled := chat.Exists || agent.Exists ||
		slices.Contains(authentikConsumers, integrations.AgenticIAMAuthentikConsumer) ||
		slices.Contains(mariadbConsumers, global.VaultMariaDBConsumerAgenticIAM)
	switch {
	case running && chat.Image == image && agent.Image == image:
		fmt.Printf("   Open %s and log in as a persona (alice / %s)\n", agenticIAMChatPublicURL, integrations.AuthentikPersonaPassword)
	case running:
		fmt.Println("   The containers run an older image: hal vault agentic-iam update")
	case !enabled && prereqErr != nil:
		fmt.Printf("❌ %v\n", prereqErr)
	case !enabled:
		fmt.Println("   hal vault agentic-iam enable")
	default:
		fmt.Println("   The lab is partly deployed: hal vault agentic-iam update (or disable)")
	}
}

func printAgenticIAMContainerLine(label, name string, c agenticIAMContainerInfo, detail, image string) {
	switch {
	case !c.Exists:
		fmt.Printf("  ⚪ %-16s : %s not deployed\n", label, name)
	case c.Status != "running":
		fmt.Printf("  🟡 %-16s : %s %s (hal vault agentic-iam update)\n", label, name, strings.ToUpper(c.Status))
	case c.Image != image:
		fmt.Printf("  🟡 %-16s : %s, %s, older image %s\n", label, name, detail, c.Image)
	default:
		fmt.Printf("  ✅ %-16s : %s, %s\n", label, name, detail)
	}
}
