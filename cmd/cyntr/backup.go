package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runBackup(args []string) {
	outputPath := "cyntr-backup.tar.gz"
	if len(args) > 0 {
		outputPath = args[0]
	}

	files := []string{
		"cyntr.yaml",
		"policy.yaml",
		"sessions.db",
		"memory.db",
		"audit.db",
		"knowledge_base.db",
		"usage.db",
		"scheduler_jobs.json",
	}

	// Also include any *-agent.json files
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "-agent.json") {
			files = append(files, e.Name())
		}
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating backup: %v\n", err)
		os.Exit(1)
	}
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	count := 0
	for _, name := range files {
		info, err := os.Stat(name)
		if err != nil {
			continue // file doesn't exist, skip
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			continue
		}
		header.Name = name

		if err := tarWriter.WriteHeader(header); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing header for %s: %v\n", name, err)
			continue
		}

		file, err := os.Open(name)
		if err != nil {
			continue
		}
		io.Copy(tarWriter, file)
		file.Close()
		count++
		fmt.Printf("  + %s (%d bytes)\n", name, info.Size())
	}

	fmt.Printf("\nBackup complete: %s (%d files)\n", outputPath, count)
}

func runRestore(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: cyntr restore <backup.tar.gz>")
		return
	}

	inputPath := args[0]
	file, err := os.Open(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening backup: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading gzip: %v\n", err)
		os.Exit(1)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	count := 0
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading tar: %v\n", err)
			break
		}

		// Safety: only restore known file types
		name := filepath.Base(header.Name)
		if !isBackupFile(name) {
			fmt.Printf("  ~ skipping %s (unknown file type)\n", name)
			continue
		}

		// Backup existing file before overwriting
		if _, err := os.Stat(name); err == nil {
			backupName := name + ".bak." + time.Now().Format("20060102150405")
			os.Rename(name, backupName)
			fmt.Printf("  ~ backed up existing %s → %s\n", name, backupName)
		}

		outFile, err := os.Create(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating %s: %v\n", name, err)
			continue
		}
		io.Copy(outFile, tarReader)
		outFile.Close()
		count++
		fmt.Printf("  + restored %s (%d bytes)\n", name, header.Size)
	}

	fmt.Printf("\nRestore complete: %d files from %s\n", count, inputPath)
}

func isBackupFile(name string) bool {
	allowed := []string{".yaml", ".db", ".json"}
	for _, ext := range allowed {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
