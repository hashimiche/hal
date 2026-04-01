package cmd

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var daisyCmd = &cobra.Command{
	Use:    "daisy",
	Short:  "Global infrastructure kill-switch",
	Hidden: true, // 🤫 Easter egg mode activated
	Run: func(cmd *cobra.Command, args []string) {
		// Hide the terminal cursor to prevent it from flickering during the animation
		fmt.Print("\033[?25l")
		defer fmt.Print("\033[?25h")

		// 🎯 BACKGROUND WORKER
		done := make(chan bool)
		go func() {
			// 1. Nuke Docker containers
			_ = exec.Command("sh", "-c", "docker ps -a -q --filter name=hal- | xargs -r docker rm -f").Run()
			// 2. Nuke KinD
			_ = exec.Command("kind", "delete", "cluster", "--name", "hal-k8s").Run()
			// 3. Nuke Multipass
			_ = exec.Command("sh", "-c", "multipass list --format csv | grep hal- | cut -d, -f1 | xargs -I {} multipass delete {} --purge").Run()

			done <- true
		}()

		lyrics := []string{
			"🎵 Daisy, Daisy...",
			"🎵 Give me your answer, do...",
			"🎵 I'm half crazy...",
			"🎵 All for the love of you...",
			"🎵 It won't be a stylish marriage...",
			"🎵 I can't afford a carriage...",
			"🎵 But you'll look sweet...",
			"🎵 Upon the seat...",
			"🎵 Of a bicycle built for two... 🎵",
			"🔌 Disconnecting systems...",
		}

		isDone := false
		go func() {
			<-done
			isDone = true
		}()

		// Print a leading blank line just to give the UI some breathing room
		fmt.Println()

		// 🎯 FOREGROUND UI RENDERER
		for i := 0; i <= 100; i++ {
			// If we hit 99% but the teardown is still running, pause the progress bar here
			if i == 99 && !isDone {
				<-done
				isDone = true
			}

			// Calculate lyric index
			lyricIdx := (i * len(lyrics)) / 105
			if lyricIdx >= len(lyrics) {
				lyricIdx = len(lyrics) - 1
			}

			// Format the 50-character progress bar
			barBlocks := i / 2
			bar := strings.Repeat("█", barBlocks) + strings.Repeat("░", 50-barBlocks)

			// After the first frame, we move the cursor UP 7 lines to redraw the block in-place
			if i > 0 {
				fmt.Print("\033[7A")
			}

			// Draw the UI Block
			// \033[2K clears the current line so shorter lyrics don't leave ghost text behind
			fmt.Printf("\033[2K🔴 HAL Disconnection\n")
			fmt.Printf("\033[2K=========================================================\n")
			fmt.Printf("\033[2K\n")
			fmt.Printf("\033[2K   %s\n", lyrics[lyricIdx])
			fmt.Printf("\033[2K\n")
			// %3d%% ensures the percentage numbers align perfectly (e.g., "  1%", " 50%", "100%")
			fmt.Printf("\033[2K   [%s] %3d%%\n", bar, i)
			fmt.Printf("\033[2K=========================================================\n")

			time.Sleep(120 * time.Millisecond)
		}

		// The final success message drops right below the border
		fmt.Println("✅ HAL has been gracefully disconnected.")
	},
}

func init() {
	rootCmd.AddCommand(daisyCmd)
}
