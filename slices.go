package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Based on https://lawlessguy.wordpress.com/2013/07/23/filling-a-slice-using-command-line-flags-in-go-golang/ & https://stackoverflow.com/questions/28322997/how-to-get-a-list-of-values-into-a-flag-in-golang

// IntSlice is a slice of ints that implements the flag.Value interface.
type IntSlice []int

func (i *IntSlice) String() string {
	return fmt.Sprintf("%d", *i)
}

// Set parses a flag value into the slice.
func (i *IntSlice) Set(value string) error {
	tmp, err := strconv.Atoi(value)
	if err != nil {
		*i = append(*i, -1)
	} else {
		*i = append(*i, tmp)
	}
	return nil
}

// StringSlice is a slice of strings that implements the flag.Value interface.
type StringSlice []string

func (s *StringSlice) String() string {
	b := strings.Builder{}
	for _, v := range *s {
		b.WriteString(ellipticalTruncate(v, 15))
		b.WriteString(":")
	}
	return b.String()
}

// Set parses a flag value into the slice.
func (s *StringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func ellipticalTruncate(text string, maxLen int) string {
	// https://stackoverflow.com/questions/59955085/how-can-i-elliptically-truncate-text-in-golang
	lastSpaceIx := maxLen
	curLen := 0
	for i, r := range text {
		if unicode.IsSpace(r) {
			lastSpaceIx = i
		}
		curLen++
		if curLen > maxLen {
			return text[:lastSpaceIx] + "…"
		}
	}
	return text
}
