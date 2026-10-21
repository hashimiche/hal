package creds

import (
	"fmt"
	"os"

	"hal/internal/global"
	"hal/internal/labsecrets"
	"hal/internal/labsecrets/live"

	"github.com/spf13/cobra"
)

const (
	boundaryContainer        = "hal-boundary"
	boundaryMariaDBContainer = "hal-boundary-target-mariadb"
	boundarySSHInstance      = "hal-boundary-ssh"
	boundaryURL              = "http://boundary.localhost:9200"
)

var Cmd = &cobra.Command{
	Use:   "creds",
	Short: "Manage active lab credentials",
	Long:  `Commands for inspecting active credentials across running lab services.`,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active lab credentials",
	Long:  `Display credentials for all currently running lab services. Only active services are shown.`,
	Run: func(cmd *cobra.Command, args []string) {
		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		tree := activeTree(engine)
		if !tree.Active() {
			fmt.Println("No active lab services detected.")
			fmt.Println("Start a service first, e.g.:")
			fmt.Println("  hal vault create")
			fmt.Println("  hal vault oidc enable")
			return
		}

		labsecrets.Render(os.Stdout, tree)
	},
}

func init() {
	Cmd.AddCommand(statusCmd)
}

// ActiveCredentials is the structured form of `hal creds status`, suitable for
// programmatic consumers (e.g. the read-only MCP credentials tool). It is the
// same tree the terminal renders, so the two surfaces cannot drift.
type ActiveCredentials struct {
	AnyActive bool                 `json:"any_active"`
	Services  []labsecrets.Section `json:"services"`
}

// CollectActiveCredentials returns the structured equivalent of `hal creds status`.
func CollectActiveCredentials() (ActiveCredentials, error) {
	engine, err := global.DetectEngine()
	if err != nil {
		return ActiveCredentials{}, err
	}

	tree := activeTree(engine)
	return ActiveCredentials{AnyActive: tree.Active(), Services: tree.Sections}, nil
}

// activeTree is the shared credential tree plus Boundary, which has no catalog
// entry of its own yet.
func activeTree(engine string) labsecrets.Tree {
	tree := live.Tree(engine)
	if boundary, ok := boundarySection(engine); ok {
		tree.Sections = append(tree.Sections, boundary)
	}
	return tree
}

// boundarySection describes a running Boundary controller, with the SSH lab
// users and the MariaDB target when those exist.
func boundarySection(engine string) (labsecrets.Section, bool) {
	if !global.CheckContainer(engine, boundaryContainer) {
		return labsecrets.Section{}, false
	}

	boundary := labsecrets.Section{
		Key:     "boundary",
		Icon:    "🔒",
		Label:   "Boundary",
		URL:     boundaryURL,
		Entries: []labsecrets.Entry{{Name: "Admin", Username: "admin", Secret: "password"}},
	}

	if global.MultipassInstanceExists(boundarySSHInstance) {
		boundary.Entries = append(boundary.Entries,
			labsecrets.Entry{Username: "ssh-operator", Secret: "password", Note: "Boundary auth method: ssh-lab-auth"},
			labsecrets.Entry{Username: "ssh-auditor", Secret: "password", Note: "Boundary auth method: ssh-lab-auth"},
		)
	}
	if global.CheckContainer(engine, boundaryMariaDBContainer) {
		boundary.Entries = append(boundary.Entries,
			labsecrets.Entry{Name: "MariaDB target", Username: "admin", Secret: "password", Note: "host: localhost:3306"},
		)
	}

	return boundary, true
}
