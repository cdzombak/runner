package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	minLenForCensorHint = 5
)

func hiddenEnvVars() []string {
	return strings.Split(os.Getenv(HideEnvVarsEnvVar), ":")
}

func censoredEnvVars() []string {
	return strings.Split(os.Getenv(CensorEnvVarsEnvVar), ":")
}

func shouldHideEnvVar(varName string) bool {
	return stringSliceContains(hiddenEnvVars(), varName)
}

func censoredEnvVarValue(varName, value string) string {
	if !stringSliceContains(censoredEnvVars(), varName) && varName != SMTPPassEnvVar {
		return value
	}
	if len(value) < minLenForCensorHint {
		return fmt.Sprintf("[%d chars]", len(value))
	}
	return fmt.Sprintf("%c[%d chars]%c", value[0], len(value)-2, value[len(value)-1])
}

func stringSliceContains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}
