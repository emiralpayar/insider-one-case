// Package verr defines structured validation errors.
package verr

import "strings"

// Error is a single validation problem.
type Error struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// List is a collection of validation errors.
type List []Error

func (l List) Error() string {
	parts := make([]string, len(l))
	for i, e := range l {
		parts[i] = e.Path + ": " + e.Message
	}
	return strings.Join(parts, "; ")
}
