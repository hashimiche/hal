package vault

import (
	"fmt"

	"hal/internal/global"

	vault "github.com/hashicorp/vault/api"
	"github.com/spf13/cobra"
)

var (
	userpassEnable  bool
	userpassDisable bool
	userpassUpdate  bool
)

var vaultUserpassCmd = &cobra.Command{
	Use:     "userpass [status|enable|disable|update]",
	Aliases: []string{"up"},
	Short:   "Configure Vault userpass auth method with a demo user and token metadata",
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := parseLifecycleAction(args, &userpassEnable, &userpassDisable, &userpassUpdate); err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}

		engine, err := global.DetectEngine()
		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			return
		}

		client, vaultErr := GetHealthyClient()

		// ==========================================
		// 1. SMART STATUS MODE (Default behavior)
		// ==========================================
		if !userpassEnable && !userpassDisable && !userpassUpdate {
			fmt.Println("🔍 Checking Vault Userpass Auth Status...")

			authMounted := false
			if vaultErr == nil {
				auths, _ := client.Sys().ListAuth()
				_, authMounted = auths["userpass/"]
			}

			if authMounted {
				fmt.Printf("  ✅ Vault Auth    : Configured (userpass/)\n")
			} else {
				fmt.Printf("  ❌ Vault Auth    : Not configured\n")
			}

			fmt.Println("\n💡 Next Step:")
			if !authMounted {
				fmt.Println("   To enable userpass auth and create a demo user, run:")
				fmt.Println("   hal vault userpass enable")
			} else {
				fmt.Println("   Demo is ready! Log in as the demo user:")
				fmt.Println("   vault login -method=userpass username=michaelScott password=threat-level-midnight")
				fmt.Println("\n   Inspect the token metadata that persisted through auth:")
				fmt.Println("   vault token lookup")
				fmt.Println("\n   To remove this auth method, run:")
				fmt.Println("   hal vault userpass disable")
			}
			return
		}

		// ==========================================
		// 2. TEARDOWN / RESET PATH (--disable / --update)
		// ==========================================
		if userpassDisable || userpassUpdate {
			if global.DryRun {
				fmt.Println("[DRY RUN] Would call API to disable userpass auth method and delete policy")
			} else {
				if userpassDisable {
					fmt.Println("🛑 Tearing down userpass auth method...")
				} else {
					fmt.Println("♻️  Update requested. Destroying userpass configuration for reset...")
				}

				if vaultErr == nil && client != nil {
					fmt.Println("⚙️  Connecting to Vault API for cleanup...")
					_ = client.Sys().DisableAuth("userpass")
					_ = client.Sys().DeletePolicy("userpass-demo")
					if s, _ := client.Logical().Write("identity/lookup/entity", map[string]any{"name": "michaelScott"}); s != nil {
						if id, ok := s.Data["id"].(string); ok {
							_, _ = client.Logical().Delete("identity/entity/id/" + id)
						}
					}
				} else {
					fmt.Println("⚠️  Vault is offline. Skipped Vault-internal cleanup.")
				}

				if userpassDisable {
					fmt.Println("✅ Userpass auth method removed successfully!")
					global.RefreshHalStatus(engine)
				}
			}

			if userpassDisable && !global.DryRun {
				return
			}
		}

		// ==========================================
		// 3. DEPLOY / ENABLE PATH (--enable / --update)
		// ==========================================
		if userpassEnable || userpassUpdate {
			if vaultErr != nil {
				fmt.Printf("❌ Cannot deploy: Vault must be running and healthy. %v\n", vaultErr)
				return
			}

			if global.DryRun {
				fmt.Println("[DRY RUN] Would enable userpass auth method.")
				fmt.Println("[DRY RUN] Would create demo policy and michaelScott with token metadata.")
				return
			}

			// 1. Enable userpass auth method
			fmt.Println("⚙️  Enabling userpass auth method...")
			_ = client.Sys().DisableAuth("userpass")
			if err = client.Sys().EnableAuthWithOptions("userpass", &vault.EnableAuthOptions{
				Type: "userpass",
			}); err != nil {
				fmt.Printf("❌ Failed to enable userpass auth: %v\n", err)
				return
			}

			// 2. Create a policy for the demo user
			fmt.Println("⚙️  Creating demo policy...")
			demoPolicy := `
path "*" { capabilities = ["create", "read", "update", "delete", "list"] }
`
			if err = client.Sys().PutPolicy("userpass-demo", demoPolicy); err != nil {
				fmt.Printf("❌ Failed to create policy: %v\n", err)
				return
			}

			// 3. Create the userpass credential
			fmt.Println("⚙️  Creating userpass user 'michaelScott'...")
			_, err = client.Logical().Write("auth/userpass/users/michaelScott", map[string]any{
				"password":       "threat-level-midnight",
				"token_policies": "userpass-demo",
			})
			if err != nil {
				fmt.Printf("❌ Failed to create user: %v\n", err)
				return
			}

			// 4. Create an Identity entity with metadata.
			//
			// userpass has no native metadata field on the user record. Metadata that
			// persists through every auth attempt must live on the Identity entity.
			// Vault attaches this entity to every token issued for the user, and the
			// metadata appears in audit log auth entries and is reachable via
			// identity/lookup/entity or vault token lookup (entity_id field).
			fmt.Println("⚙️  Creating Identity entity with metadata tags...")
			entitySecret, err := client.Logical().Write("identity/entity", map[string]any{
				"name": "michaelScott",
				"metadata": map[string]string{
					"branch":       "scranton",
					"company":      "dunder-mifflin",
					"role":         "regional-manager",
					"fun_fact":     "thats-what-she-said",
				},
			})
			if err != nil || entitySecret == nil {
				fmt.Printf("❌ Failed to create identity entity: %v\n", err)
				return
			}
			entityID := entitySecret.Data["id"].(string)

			// 5. Link the entity to the userpass user via an alias.
			//
			// The alias name must match the username exactly. The mount accessor
			// tells Vault which auth method this alias belongs to.
			fmt.Println("⚙️  Linking entity to userpass mount via alias...")
			auths, err := client.Sys().ListAuth()
			if err != nil {
				fmt.Printf("❌ Failed to list auth mounts: %v\n", err)
				return
			}
			accessor := auths["userpass/"].Accessor
			_, err = client.Logical().Write("identity/entity-alias", map[string]any{
				"name":           "michaelScott",
				"canonical_id":   entityID,
				"mount_accessor": accessor,
			})
			if err != nil {
				fmt.Printf("❌ Failed to create entity alias: %v\n", err)
				return
			}

			// 6. Verify by logging in
			fmt.Println("⚙️  Verifying auth by logging in as michaelScott...")
			_, err = client.Logical().Write("auth/userpass/login/michaelScott", map[string]any{
				"password": "threat-level-midnight",
			})
			if err != nil {
				fmt.Printf("❌ Login verification failed: %v\n", err)
				return
			}

			fmt.Println("\n✅ Userpass Auth Method Configured!")
			global.RefreshHalStatus(engine)
			fmt.Println("---------------------------------------------------------")
			fmt.Println("👤 Username    : michaelScott")
			fmt.Println("🔑 Password    : threat-level-midnight")
			fmt.Println("🪪 Entity ID   : " + entityID)
			fmt.Println("📋 Entity Metadata (persists on every login):")
			fmt.Println("     branch:           scranton")
			fmt.Println("     company:          dunder-mifflin")
			fmt.Println("     role:             regional-manager")
			fmt.Println("     fun_fact:         thats-what-she-said")
			fmt.Println("\n💡 Try it yourself:")
			fmt.Println("   vault login -method=userpass username=michaelScott password=threat-level-midnight")
			fmt.Println("   vault read identity/entity/id/" + entityID)
			fmt.Println("   vault write identity/lookup/entity name=michaelScott")
			fmt.Println("---------------------------------------------------------")
		}
	},
}

func init() {
	vaultUserpassCmd.Flags().BoolVarP(&userpassEnable, "enable", "e", false, "Enable userpass auth method and create a demo user")
	vaultUserpassCmd.Flags().BoolVarP(&userpassDisable, "disable", "d", false, "Disable userpass auth method and remove its policy")
	vaultUserpassCmd.Flags().BoolVarP(&userpassUpdate, "update", "u", false, "Reconcile userpass auth method configuration")
	_ = vaultUserpassCmd.Flags().MarkHidden("enable")
	_ = vaultUserpassCmd.Flags().MarkHidden("disable")
	_ = vaultUserpassCmd.Flags().MarkHidden("update")

	Cmd.AddCommand(vaultUserpassCmd)
}
