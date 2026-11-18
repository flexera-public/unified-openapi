package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func gitChangeOutput(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}
func collectChangeReport(root, ref, scope string) (ChangeReport, error) {
	report := ChangeReport{Files: []FileChange{}}
	paths := []string{"generator/sources", "openapi3.json", "openapi3.yaml"}
	switch scope {
	case "all":
	case "sources":
		paths = paths[:1]
	case "artifacts":
		paths = paths[1:]
	default:
		return report, fmt.Errorf("invalid scope %q", scope)
	}
	commit, err := gitChangeOutput(root, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return report, err
	}
	sha := strings.TrimSpace(string(commit))
	args := append([]string{"ls-tree", "-r", "--name-only", "-z", sha, "--"}, paths...)
	tracked, err := gitChangeOutput(root, args...)
	if err != nil {
		return report, err
	}
	args = append([]string{"ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}, paths...)
	current, err := gitChangeOutput(root, args...)
	if err != nil {
		return report, err
	}
	baseline := map[string]bool{}
	names := map[string]bool{}
	for _, p := range strings.Split(string(tracked), "\x00") {
		if p != "" {
			baseline[p] = true
			names[p] = true
		}
	}
	for _, p := range strings.Split(string(current), "\x00") {
		if p != "" {
			names[p] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for p := range names {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		var before, after []byte
		if baseline[name] {
			before, err = gitChangeOutput(root, "show", sha+":"+name)
			if err != nil {
				return report, err
			}
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		info, statErr := os.Lstat(path)
		if statErr == nil {
			if !info.Mode().IsRegular() {
				return report, fmt.Errorf("cannot compare nonregular file %s", name)
			}
			after, err = os.ReadFile(path)
			if err != nil {
				return report, err
			}
		} else if !os.IsNotExist(statErr) {
			return report, statErr
		}
		if bytes.Equal(before, after) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		var file FileChange
		if ext == ".json" || ext == ".yaml" || ext == ".yml" {
			file, err = compareSpecBytes(name, before, after)
			if err != nil {
				return report, err
			}
		} else {
			action := "modified"
			if before == nil {
				action = "added"
			} else if after == nil {
				action = "removed"
			}
			file = FileChange{File: name, Status: "functional", Changes: []ChangeRecord{{Path: "/", Action: action}}}
		}
		report.Files = append(report.Files, file)
		if file.Status == "functional" {
			report.HasFunctionalChanges = true
		}
	}
	return report, nil
}
func runChanges(args []string) error {
	flags := flag.NewFlagSet("changes", flag.ContinueOnError)
	base := flags.String("base-ref", "HEAD", "Git baseline ref")
	scope := flags.String("scope", "all", "all, sources, or artifacts")
	format := flags.String("format", "text", "text, markdown, or json")
	jsonOutput := flags.String("json-output", "", "Write JSON report to this file")
	markdownOutput := flags.String("markdown-output", "", "Write Markdown report to this file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *format != "text" && *format != "markdown" && *format != "json" {
		return fmt.Errorf("invalid format %q", *format)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	rootBytes, err := gitChangeOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	root := strings.TrimSpace(string(rootBytes))
	for _, output := range []string{*jsonOutput, *markdownOutput} {
		if output == "" {
			continue
		}
		abs, err := filepath.Abs(output)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("report output must be outside the working tree: %s", output)
		}
		if _, err := os.Lstat(abs); err == nil {
			return fmt.Errorf("report output already exists: %s", output)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	report, err := collectChangeReport(root, *base, *scope)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	markdown := renderChangeReport(report, true)
	if *jsonOutput != "" {
		if err := writeChangeReport(*jsonOutput, data); err != nil {
			return err
		}
	}
	if *markdownOutput != "" {
		if err := writeChangeReport(*markdownOutput, []byte(markdown)); err != nil {
			return err
		}
	}
	switch *format {
	case "json":
		_, err = os.Stdout.Write(data)
	case "markdown":
		_, err = io.WriteString(os.Stdout, markdown)
	default:
		_, err = io.WriteString(os.Stdout, renderChangeReport(report, false))
	}
	return err
}
func writeChangeReport(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
