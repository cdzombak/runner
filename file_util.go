package main

import (
	"io"
	"os"
	"strings"
)

func writeToFile(filename string, data string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data)
	if err != nil {
		return err
	}
	return file.Sync()
}

func removeBadFilenameChars(filename string) string {
	badChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "'", "<", ">", ".", " "}
	for _, v := range badChars {
		filename = strings.ReplaceAll(filename, v, "-")
	}
	return filename
}
