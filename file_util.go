package main

import (
	"strings"
)

func removeBadFilenameChars(filename string) string {
	badChars := []string{"/", "\\", "?", "%", "*", ":", "|", "\"", "'", "<", ">", ".", " "}
	for _, v := range badChars {
		filename = strings.ReplaceAll(filename, v, "-")
	}
	return filename
}
