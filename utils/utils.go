package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fatih/color"
)

var keyColor = color.New(color.FgCyan, color.Bold)
var stringColor = color.New(color.FgGreen)
var numberColor = color.New(color.FgYellow)
var boolColor = color.New(color.FgMagenta)
var nullColor = color.New(color.FgRed)
var structureColor = color.New(color.FgWhite, color.Bold)

// colorizeAndPrintJSON prints JSON with syntax highlighting
func colorizeAndPrintJSON(jsonStr string) {
	lines := strings.Split(jsonStr, "\n")
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		
		if trimmedLine == "{" || trimmedLine == "}" || 
		   trimmedLine == "[" || trimmedLine == "]" || 
		   trimmedLine == "," || trimmedLine == "}," || 
		   trimmedLine == "]," {
			structureColor.Println(line)
			continue
		}
		
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key := parts[0]
			value := strings.TrimSpace(parts[1])
			
			keyColor.Print(key)
			fmt.Print(":")
			
			if strings.HasSuffix(value, "{") || strings.HasSuffix(value, "[") {
				valueWithoutBracket := strings.TrimSuffix(strings.TrimSuffix(value, "{"), "[")
				if valueWithoutBracket != "" {
					fmt.Print(valueWithoutBracket)
				}
				structureColor.Println(value[len(valueWithoutBracket):])
				continue
			}
			
			if strings.HasSuffix(value, "}") || strings.HasSuffix(value, "]") || strings.HasSuffix(value, "},") || strings.HasSuffix(value, "],") {
				lastBracketPos := -1
				for i := len(value) - 1; i >= 0; i-- {
					if value[i] == '}' || value[i] == ']' {
						lastBracketPos = i
						break
					}
				}
				
				if lastBracketPos > 0 {
					valueBeforeBracket := value[:lastBracketPos]
					bracketPart := value[lastBracketPos:]
					
					colorizeValue(valueBeforeBracket)
					structureColor.Println(bracketPart)
				} else {
					structureColor.Println(value)
				}
				continue
			}
			
			colorizeValue(value)
		} else {
			colorizeValue(line)
		}
	}	
}

// Helper function to colorize values based on their type
func colorizeValue(value string) {
	trimmedValue := strings.TrimSpace(value)
	
	switch {
	case strings.HasPrefix(trimmedValue, "\"") && (strings.HasSuffix(trimmedValue, "\"") || strings.HasSuffix(trimmedValue, "\",")):
		stringColor.Println(value)
	case trimmedValue == "true" || trimmedValue == "false" || 
	     strings.HasSuffix(trimmedValue, "true,") || strings.HasSuffix(trimmedValue, "false,"):
		boolColor.Println(value)
	case trimmedValue == "null" || strings.HasSuffix(trimmedValue, "null,"):
		nullColor.Println(value)
	case strings.HasPrefix(trimmedValue, "{") || strings.HasPrefix(trimmedValue, "["):
		structureColor.Println(value)
	default:
		numberColor.Println(value)
	}
}

func FormatAndPrintResponse(response any) error {
	prettyJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Errorf("error formatting JSON: %v", err)
	}
	
	colorizeAndPrintJSON(string(prettyJSON))
	return nil
}