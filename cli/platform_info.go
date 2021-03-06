package cli

import (
	"context"
	"flag"

	"github.com/johnewart/subcommands"
	"github.com/matishsiao/goInfo"
)

type PlatformCmd struct {
}

func (*PlatformCmd) Name() string     { return "platform" }
func (*PlatformCmd) Synopsis() string { return "Show platform info." }
func (*PlatformCmd) Usage() string {
	return `platform`
}

func (p *PlatformCmd) SetFlags(f *flag.FlagSet) {
}

func (p *PlatformCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	gi := goInfo.GetInfo()
	gi.VarDump()
	return subcommands.ExitSuccess
}
