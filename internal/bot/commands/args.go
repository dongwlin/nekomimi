package commands

import "strings"

func parseActionArgs(args string) (string, string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	fields := strings.Fields(args)
	action := strings.ToLower(fields[0])
	rest := strings.TrimSpace(args[len(fields[0]):])
	return action, rest
}
