package main

import (
	"io"
	"os"
	"strings"
)

func writeLogFile(filename, data string) error {
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, data)
	return err
}

func removeBadFilenameChars(filename string) string {
	badChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "'", "<", ">", ".", " "}
	for _, v := range badChars {
		filename = strings.ReplaceAll(filename, v, "-")
	}
	return filename
}
