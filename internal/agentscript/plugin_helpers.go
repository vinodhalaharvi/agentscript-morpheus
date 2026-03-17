package agentscript

// firstArg safely returns args[0] or empty string.
func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// secondArg safely returns args[1] or empty string.
func secondArg(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return ""
}

// thirdArg safely returns args[2] or empty string.
func thirdArg(args []string) string {
	if len(args) > 2 {
		return args[2]
	}
	return ""
}

// argOr returns args[n] if it exists, otherwise fallback.
func argOr(args []string, n int, fallback string) string {
	if len(args) > n && args[n] != "" {
		return args[n]
	}
	return fallback
}
