// Command appclipcodegen generates and scans Apple App Clip Code SVGs.
//
// Usage:
//
//	appclipcodegen generate -url URL [-index N] [-fg HEX -bg HEX] [-type cam|nfc] [-o FILE]
//	appclipcodegen gen URL [-index N] [-fg HEX -bg HEX] [-type cam|nfc] [-output FILE]
//	appclipcodegen scan FILE
//	appclipcodegen templates
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/rs/appclipcode"
)

const cliName = "appclipcodegen"

func main() {
	command, args := resolveCommand(os.Args[1:])
	switch command {
	case "generate":
		cmdGenerate(args)
	case "scan":
		cmdScan(args)
	case "templates":
		cmdTemplates()
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage:
  %[1]s generate -url URL [-index N] [-fg HEX -bg HEX] [options] [-o FILE | -output FILE]
  %[1]s gen URL [-index N] [-fg HEX -bg HEX] [options] [-o FILE | -output FILE]
  %[1]s scan FILE              Decode URL from an App Clip Code SVG or PNG
  %[1]s templates              List available color templates

Generate options:
  -url URL        URL to encode (must be https://)
  -fg HEX         Foreground color as 6-digit hex (e.g. 000000)
  -bg HEX         Background color as 6-digit hex (e.g. FFFFFF)
  -index N        Use predefined template color (0-17, default: 0 when -fg/-bg are omitted)
  -type TYPE      Code type: cam (default) or nfc
  -o FILE         Output file path (default: stdout)
  -output FILE    Output file path (default: stdout)
`, cliName)
}

func cmdGenerate(args []string) {
	args = normalizeGenerateArgs(args)

	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	urlFlag := fs.String("url", "", "URL to encode")
	fgFlag := fs.String("fg", "", "Foreground color hex")
	bgFlag := fs.String("bg", "", "Background color hex")
	indexFlag := fs.Int("index", 0, "Template color index (0-17)")
	typeFlag := fs.String("type", "cam", "Code type: cam or nfc")
	outShortFlag := fs.String("o", "", "Output file (default: stdout)")
	outLongFlag := fs.String("output", "", "Output file (default: stdout)")
	indexProvided := hasFlag(args, "index")
	fs.Parse(args)

	if *urlFlag == "" {
		fmt.Fprintln(os.Stderr, "error: -url is required")
		fs.Usage()
		os.Exit(1)
	}

	opts := &appclipcode.Options{
		Type: appclipcode.CodeType(*typeFlag),
	}

	svg, err := generateSVG(*urlFlag, *fgFlag, *bgFlag, *indexFlag, indexProvided, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	outputPath, err := resolveOutputPath(*outShortFlag, *outLongFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, svg, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "App Clip Code successfully generated.")
	} else {
		os.Stdout.Write(svg)
	}
}

func resolveCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "help", nil
	}

	switch args[0] {
	case "generate", "gen":
		return "generate", args[1:]
	case "scan":
		return "scan", args[1:]
	case "templates":
		return "templates", args[1:]
	case "-h", "--help", "help":
		return "help", nil
	default:
		return "generate", args
	}
}

func normalizeGenerateArgs(args []string) []string {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return args
	}

	return append([]string{"-url", args[0]}, args[1:]...)
}

func hasFlag(args []string, name string) bool {
	short := "-" + name
	long := "--" + name
	shortEq := short + "="
	longEq := long + "="

	for _, arg := range args {
		switch {
		case arg == short, arg == long:
			return true
		case strings.HasPrefix(arg, shortEq), strings.HasPrefix(arg, longEq):
			return true
		}
	}

	return false
}

func resolveOutputPath(shortPath, longPath string) (string, error) {
	switch {
	case shortPath != "" && longPath != "" && shortPath != longPath:
		return "", fmt.Errorf("specify only one of -o or -output")
	case longPath != "":
		return longPath, nil
	default:
		return shortPath, nil
	}
}

func generateSVG(url, fg, bg string, index int, indexProvided bool, opts *appclipcode.Options) ([]byte, error) {
	switch {
	case fg == "" && bg == "":
		return appclipcode.GenerateWithTemplate(url, index, opts)
	case fg != "" && bg != "":
		if indexProvided {
			return nil, fmt.Errorf("specify either -fg/-bg or -index, not both")
		}
		return appclipcode.Generate(url, fg, bg, opts)
	default:
		return nil, fmt.Errorf("specify both -fg and -bg")
	}
}

func cmdScan(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "error: scan requires a file path")
		fmt.Fprintf(os.Stderr, "usage: %s scan FILE\n", cliName)
		os.Exit(1)
	}

	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
		os.Exit(1)
	}

	url, err := appclipcode.ReadImage(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error scanning: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(url)
}

func cmdTemplates() {
	for _, t := range appclipcode.Templates() {
		fmt.Printf("Index: %2d  Foreground: %02X%02X%02X  Background: %02X%02X%02X\n",
			t.Index, t.Foreground.R, t.Foreground.G, t.Foreground.B,
			t.Background.R, t.Background.G, t.Background.B)
	}
}
