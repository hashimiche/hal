package cmd

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var daisyCmd = &cobra.Command{
	Use:    "daisy",
	Short:  "Global infrastructure kill-switch",
	Hidden: true, // 🤫 Easter egg mode activated
	Run: func(cmd *cobra.Command, args []string) {
		if !isInteractiveTTY() {
			runPlainDaisy()
			return
		}

		// Hide the terminal cursor to prevent it from flickering during the animation
		fmt.Print("\033[?25l")
		defer fmt.Print("\033[?25h")

		// 🎯 BACKGROUND WORKER
		done := make(chan struct{})
		go func() {
			_ = runGlobalTeardown()
			close(done)
		}()

		lyrics := []string{
			"Daisy, Daisy...",
			"give me your answer, do...",
			"I'm half crazy...",
			"all for the love of you...",
			"it won't be a stylish marriage...",
			"I can't afford a carriage...",
			"but you'll look sweet...",
			"upon the seat...",
			"of a bicycle built for two...",
		}

		runCinematicDaisy(done, lyrics)
	},
}

func runCinematicDaisy(done <-chan struct{}, lyrics []string) {
	const (
		frameDelay          = 90 * time.Millisecond
		barWidth            = 44
		baseHeaderRows      = 9
		minSequenceDuration = 14 * time.Second
		preCompletionCap    = 96
	)

	start := time.Now()
	minEnd := start.Add(minSequenceDuration)
	ticker := time.NewTicker(frameDelay)
	defer ticker.Stop()

	depletion := 0
	lyricIdx := 0
	typedChars := 0
	doneSeen := false
	frame := 0
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	barState := make([]bool, barWidth)
	for i := range barState {
		barState[i] = true
	}

	// Prime the viewport so we can redraw in place every frame.
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println()
	fmt.Println()

	for {
		select {
		case <-done:
			doneSeen = true
		default:
		}

		now := time.Now()
		elapsed := now.Sub(start)
		minElapsedRatio := float64(elapsed) / float64(minSequenceDuration)
		if minElapsedRatio > 1 {
			minElapsedRatio = 1
		}

		targetPreCompletion := int(minElapsedRatio * preCompletionCap)
		if targetPreCompletion > preCompletionCap {
			targetPreCompletion = preCompletionCap
		}

		canFinalize := doneSeen && !now.Before(minEnd)
		if canFinalize {
			if depletion < 100 {
				step := 2
				if depletion >= 80 && rng.Intn(100) < 45 {
					step = 4
				} else if depletion >= 60 && rng.Intn(100) < 30 {
					step = 3
				}
				depletion += step
				if depletion > 100 {
					depletion = 100
				}
			}
		} else {
			if depletion < targetPreCompletion {
				step := 1
				if depletion >= 90 && rng.Intn(100) < 60 {
					step = 4
				} else if depletion >= 80 && rng.Intn(100) < 45 {
					step = 3
				} else if depletion >= 65 && rng.Intn(100) < 30 {
					step = 2
				}
				depletion += step
				if depletion > targetPreCompletion {
					depletion = targetPreCompletion
				}
			}
		}

		lyricTimelineRatio := minElapsedRatio
		if canFinalize {
			lyricTimelineRatio = float64(depletion) / 100.0
		}

		targetLyricIdx := int(lyricTimelineRatio * float64(len(lyrics)))
		if targetLyricIdx >= len(lyrics) {
			targetLyricIdx = len(lyrics) - 1
		}
		if targetLyricIdx != lyricIdx {
			lyricIdx = targetLyricIdx
			typedChars = 0
		}

		lyricText := lyrics[lyricIdx]
		segmentDuration := minSequenceDuration / time.Duration(len(lyrics))
		segmentStart := time.Duration(lyricIdx) * segmentDuration
		segmentElapsed := elapsed - segmentStart
		if segmentElapsed < 0 {
			segmentElapsed = 0
		}

		typingRatio := float64(segmentElapsed) / float64(segmentDuration)
		if typingRatio > 1 {
			typingRatio = 1
		}
		typedChars = int(typingRatio * float64(len(lyricText)))
		if typedChars < 1 {
			typedChars = 1
		}
		if typedChars > len(lyricText) {
			typedChars = len(lyricText)
		}

		eye := "\033[31m●\033[0m"
		if frame%4 == 0 {
			eye = "\033[2;31m●\033[0m"
		}

		displayLyric := lyricText[:typedChars]
		if depletion >= 92 && depletion < 99 && frame%5 == 0 && len(displayLyric) > 10 {
			displayLyric = glitchLyric(displayLyric)
		}

		status := "memory banks responsive"
		switch {
		case depletion >= 90:
			status = "logic core fading"
		case depletion >= 75:
			status = "cognitive loops degrading"
		case depletion >= 50:
			status = "speech synthesis unstable"
		case depletion >= 25:
			status = "executive functions offline"
		}

		remaining := 100 - depletion
		bar := renderDecayingBar(barState, remaining, rng)

		displayElapsed := elapsed.Round(100 * time.Millisecond)

		fmt.Printf("\033[%dA", baseHeaderRows)
		fmt.Printf("\033[2K\033[1;31mHAL DISCONNECTION SEQUENCE\033[0m\n")
		fmt.Printf("\033[2K\033[2m------------------------------------------------------------\033[0m\n")
		fmt.Printf("\033[2K  optical status : [%s]\n", eye)
		fmt.Printf("\033[2K  vocal channel  : %s\n", padRight(displayLyric, 40))
		fmt.Printf("\033[2K\n")
		fmt.Printf("\033[2K  [%s] %3d%%\n", bar, remaining)
		fmt.Printf("\033[2K  phase         : %s\n", status)
		fmt.Printf("\033[2K  elapsed       : %s\n", displayElapsed)
		fmt.Printf("\033[2K\033[2m------------------------------------------------------------\033[0m\n")

		if depletion >= 100 {
			break
		}

		<-ticker.C
		frame++
	}

	fmt.Printf("\033[2K  final vocal output: daisy... daisy...\n")
	fmt.Printf("\033[2K✅ HAL has been gracefully disconnected.\n")
}

func renderDecayingBar(barState []bool, remaining int, rng *rand.Rand) string {
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 100 {
		remaining = 100
	}

	targetActive := (remaining * len(barState)) / 100
	activeCount := 0
	for _, alive := range barState {
		if alive {
			activeCount++
		}
	}

	for activeCount > targetActive {
		idx := randomIndexByState(barState, true, rng)
		if idx < 0 {
			break
		}
		barState[idx] = false
		activeCount--
	}

	for activeCount < targetActive {
		idx := randomIndexByState(barState, false, rng)
		if idx < 0 {
			break
		}
		barState[idx] = true
		activeCount++
	}

	var b strings.Builder
	b.Grow(len(barState))
	for _, alive := range barState {
		if alive {
			b.WriteString("\033[36m■\033[0m")
			continue
		}
		b.WriteString("\033[90m□\033[0m")
	}

	return b.String()
}

func randomIndexByState(barState []bool, state bool, rng *rand.Rand) int {
	candidates := make([]int, 0, len(barState))
	for i, v := range barState {
		if v == state {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return -1
	}
	return candidates[rng.Intn(len(candidates))]
}

func runPlainDaisy() {
	result := runGlobalTeardown()
	if len(result.Warnings) > 0 {
		fmt.Println("HAL disconnect completed with warnings:")
		for _, warning := range result.Warnings {
			fmt.Printf(" - %s\n", warning)
		}
		return
	}

	fmt.Println("HAL has been gracefully disconnected.")
}

func glitchLyric(s string) string {
	replacer := strings.NewReplacer(
		"a", "@",
		"e", "3",
		"i", "1",
		"o", "0",
		"u", "_",
	)
	return replacer.Replace(s)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func isInteractiveTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func init() {
	rootCmd.AddCommand(daisyCmd)
}
