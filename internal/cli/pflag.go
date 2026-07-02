package cli

import "github.com/spf13/pflag"

type flagSet interface {
	Lookup(string) *pflag.Flag
}
