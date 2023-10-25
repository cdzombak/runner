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
	retv := strings.Split(os.Getenv(CensorEnvVarsEnvVar), ":")
	retv = append(retv, SMTPPassEnvVar)
	retv = append(retv, NtfyAccessTokenEnvVar)
	return retv
}

func shouldHideEnvVar(varName string) bool {
	return stringSliceContains(hiddenEnvVars(), varName)
}

func censoredEnvVarValue(varName, value string) string {
	if !stringSliceContains(censoredEnvVars(), varName) {
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
