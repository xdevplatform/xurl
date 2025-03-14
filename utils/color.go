package utils

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// colorizeAndPrintJSON prints JSON with syntax highlighting
func ColorizeAndPrintJSON(jsonStr string) {
	// Define colors
	keyColor := color.New(color.FgCyan, color.Bold)
	stringColor := color.New(color.FgGreen)
	numberColor := color.New(color.FgYellow)
	boolColor := color.New(color.FgMagenta)
	nullColor := color.New(color.FgRed)
	
	lines := strings.Split(jsonStr, "\n")
	for _, line := range lines {
		// Process each line
		if strings.Contains(line, ":") {
			// This is a key-value pair
			parts := strings.SplitN(line, ":", 2)
			key := parts[0]
			value := parts[1]
			
			// Print key in cyan
			keyColor.Print(key)
			fmt.Print(":")
			
			// Colorize value based on type
			switch {
			case strings.Contains(value, "\""):
				// String value
				stringColor.Println(value)
			case strings.Contains(value, "true") || strings.Contains(value, "false"):
				// Boolean value
				boolColor.Println(value)
			case strings.Contains(value, "null"):
				// Null value
				nullColor.Println(value)
			default:
				// Assume number
				numberColor.Println(value)
			}
		} else {
			// This is structure (braces, brackets)
			fmt.Println(line)
		}
	}
}