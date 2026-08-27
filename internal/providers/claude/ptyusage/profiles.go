package ptyusage

import (
	"fmt"

	"windowseat/internal/providers/versionprofile"
)

const (
	MinimumTestedVersion          = "2.1.245"
	CaptureWidth                  = 120
	CaptureHeight                 = 40
	ParserProfileClaudeCodeTwoOne = "claude_code_2_1_245"
)

var parserProfiles = versionprofile.MustNew([]versionprofile.Definition[parserImplementation]{
	{
		ID:            ParserProfileClaudeCodeTwoOne,
		PreferredFrom: MinimumTestedVersion,
		Value: parserImplementation{
			width:        CaptureWidth,
			height:       CaptureHeight,
			screenReader: true,
			parse:        parseClaudeCodeTwoOneTwoFortyFive,
		},
	},
})

type ParserProfile struct {
	ID                   string
	PreferredFrom        string
	VersionBeforeProfile bool
	Width                int
	Height               int
	ScreenReader         bool
}

type parserImplementation struct {
	width        int
	height       int
	screenReader bool
	parse        func(ParseInput, []string) (Usage, error)
}

type selectedParser struct {
	ParserProfile
	parse func(ParseInput, []string) (Usage, error)
}

func ParserProfileForVersion(version string) (ParserProfile, error) {
	profile, err := selectParser(version)
	return profile.ParserProfile, err
}

func selectParser(version string) (selectedParser, error) {
	selection, err := parserProfiles.Select(version)
	if err != nil {
		return selectedParser{}, fmt.Errorf("%w: %s", ErrInvalidClaudeVersion, version)
	}
	return selectedParser{
		ParserProfile: ParserProfile{
			ID:                   selection.ID,
			PreferredFrom:        selection.PreferredFrom,
			VersionBeforeProfile: selection.BeforePreferred,
			Width:                selection.Value.width,
			Height:               selection.Value.height,
			ScreenReader:         selection.Value.screenReader,
		},
		parse: selection.Value.parse,
	}, nil
}
