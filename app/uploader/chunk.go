package uploader

import "github.com/wizardofsalem/wft-companion/app/savedvars"

const (
	MaxPartEntries = savedvars.MaxPartEntries
	MaxParts       = savedvars.MaxParts
)

func split(records []savedvars.Record) []savedvars.Record { return savedvars.Split(records) }
