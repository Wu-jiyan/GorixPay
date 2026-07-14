package database

import (
	"fmt"
	"strconv"
)

type Dialect interface {
	Name() string
	BindVar(position int) string
	SupportsSkipLocked() bool
}

type dialect struct {
	name       string
	postgres   bool
	skipLocked bool
}

func NewDialect(name string) (Dialect, error) {
	switch name {
	case "sqlite":
		return dialect{name: name}, nil
	case "mysql":
		return dialect{name: name}, nil
	case "postgres":
		return dialect{name: name, postgres: true, skipLocked: true}, nil
	default:
		return nil, fmt.Errorf("unsupported database dialect %q", name)
	}
}

func (d dialect) Name() string {
	return d.name
}

func (d dialect) BindVar(position int) string {
	if d.postgres {
		return "$" + strconv.Itoa(position)
	}
	return "?"
}

func (d dialect) SupportsSkipLocked() bool {
	return d.skipLocked
}
