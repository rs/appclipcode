package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/appclipcode"
)

func TestGenerateSVGDefaultsToTemplateZero(t *testing.T) {
	got, err := generateSVG("https://example.com", "", "", 0, false, nil)
	if err != nil {
		t.Fatalf("generateSVG returned error: %v", err)
	}

	want, err := appclipcode.GenerateWithTemplate("https://example.com", 0, nil)
	if err != nil {
		t.Fatalf("GenerateWithTemplate returned error: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatal("generateSVG did not default to template index 0")
	}
}

func TestGenerateSVGUsesCustomColorsWhenIndexNotProvided(t *testing.T) {
	got, err := generateSVG("https://example.com", "FFFFFF", "000000", 0, false, nil)
	if err != nil {
		t.Fatalf("generateSVG returned error: %v", err)
	}

	want, err := appclipcode.Generate("https://example.com", "FFFFFF", "000000", nil)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if !bytes.Equal(got, want) {
		t.Fatal("generateSVG ignored custom colors when index was not provided")
	}
}

func TestGenerateSVGRejectsExplicitIndexWithCustomColors(t *testing.T) {
	_, err := generateSVG("https://example.com", "FFFFFF", "000000", 0, true, nil)
	if err == nil {
		t.Fatal("generateSVG should reject explicit -index with -fg/-bg")
	}

	if !strings.Contains(err.Error(), "specify either -fg/-bg or -index") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveCommandSupportsGenAlias(t *testing.T) {
	command, args := resolveCommand([]string{"gen", "https://example.com"})
	if command != "generate" {
		t.Fatalf("resolveCommand returned command %q", command)
	}

	if len(args) != 1 || args[0] != "https://example.com" {
		t.Fatalf("resolveCommand returned args %v", args)
	}
}

func TestResolveCommandTreatsTopLevelArgsAsGenerate(t *testing.T) {
	command, args := resolveCommand([]string{"https://example.com", "-output", "code.svg"})
	if command != "generate" {
		t.Fatalf("resolveCommand returned command %q", command)
	}

	if len(args) != 3 || args[0] != "https://example.com" || args[1] != "-output" || args[2] != "code.svg" {
		t.Fatalf("resolveCommand returned args %v", args)
	}
}

func TestNormalizeGenerateArgsTreatsFirstPositionalAsURL(t *testing.T) {
	got := normalizeGenerateArgs([]string{"https://example.com", "-output", "code.svg"})
	want := []string{"-url", "https://example.com", "-output", "code.svg"}

	if len(got) != len(want) {
		t.Fatalf("normalizeGenerateArgs returned %v", got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeGenerateArgs returned %v, want %v", got, want)
		}
	}
}

func TestResolveOutputPathUsesLongFlag(t *testing.T) {
	got, err := resolveOutputPath("", "code.svg")
	if err != nil {
		t.Fatalf("resolveOutputPath returned error: %v", err)
	}

	if got != "code.svg" {
		t.Fatalf("resolveOutputPath returned %q", got)
	}
}

func TestResolveOutputPathRejectsConflictingFlags(t *testing.T) {
	_, err := resolveOutputPath("short.svg", "long.svg")
	if err == nil {
		t.Fatal("resolveOutputPath should reject conflicting values")
	}

	if !strings.Contains(err.Error(), "specify only one of -o or -output") {
		t.Fatalf("unexpected error: %v", err)
	}
}
