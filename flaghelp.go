package main

import "flag"

// WasFlagGiven returns true if the flag was given on the command line.
func WasFlagGiven(flagName string) bool {
	retv := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			retv = true
		}
	})
	return retv
}
