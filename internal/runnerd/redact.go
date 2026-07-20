package runnerd

import "regexp"

var runnerRedactions = []*regexp.Regexp{
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]+`),
	regexp.MustCompile(`AKIA[0-9A-Z]+`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*`),
}

func redactRunnerOutput(value string) string {
	for _, pattern := range runnerRedactions {
		value = pattern.ReplaceAllString(value, "[REDACTED]")
	}
	return value
}
