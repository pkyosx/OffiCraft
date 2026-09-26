package main

import "fmt"

func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

func pyStr(v any) string {
	if v == nil {
		return "None"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
