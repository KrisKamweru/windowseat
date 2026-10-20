package claude

import (
	"errors"

	"windowseat/internal/contracts"
	"windowseat/internal/providers/command"
)

// failure is a read that produced no usable meters. Messages are fixed text:
// they never echo provider output, which could carry account details.
type failure struct {
	state   contracts.ProviderState
	code    string
	phase   contracts.ErrorPhase
	message string
	hint    string
}

var (
	commandNotFound = failure{
		state:   contracts.ProviderUnavailable,
		code:    "command_not_found",
		phase:   contracts.ErrorDiscover,
		message: "The claude command was not found on PATH.",
		hint:    "Install Claude Code or add it to the service user's PATH.",
	}
	versionUnreadable = failure{
		state:   contracts.ProviderUnavailable,
		code:    "version_unreadable",
		phase:   contracts.ErrorDiscover,
		message: "The claude command did not report a readable version.",
	}
	unsupportedVersion = failure{
		state:   contracts.ProviderUnsupportedVersion,
		code:    "version_invalid",
		phase:   contracts.ErrorDiscover,
		message: "The claude version cannot be matched to a response profile.",
	}
	commandTimedOut = failure{
		state:   contracts.ProviderUnavailable,
		code:    "usage_command_timeout",
		phase:   contracts.ErrorRead,
		message: "claude /usage did not finish before the timeout.",
	}
	commandFailed = failure{
		state:   contracts.ProviderUnavailable,
		code:    "usage_command_failed",
		phase:   contracts.ErrorRead,
		message: "claude /usage exited with an error and no output.",
	}
	commandReportedError = failure{
		state:   contracts.ProviderUnavailable,
		code:    "usage_command_error",
		phase:   contracts.ErrorRead,
		message: "claude /usage reported an error.",
	}
	notAuthenticated = failure{
		state:   contracts.ProviderAuthRequired,
		code:    "not_authenticated",
		phase:   contracts.ErrorAuthenticate,
		message: "The selected claude command is not signed in to a Claude subscription.",
		hint:    authRecoveryTip,
	}
	safetyCheckFailed = failure{
		state:   contracts.ProviderMalformedResponse,
		code:    "usage_safety_check_failed",
		phase:   contracts.ErrorParse,
		message: "claude /usage did not run as a free local command; the reading was discarded.",
	}
	streamUnreadable = malformed("usage_stream_unreadable", "claude /usage output was not valid stream JSON.")
	resultMissing    = malformed("usage_result_missing", "claude /usage output had no result event.")
	reportMissing    = malformed("usage_report_missing", "claude /usage output had no usage report.")
	reportEmpty      = malformed("usage_report_empty", "claude /usage reported no rate limits.")
	reportUnreadable = malformed("usage_report_unreadable", "claude /usage reported no readable percentage or reset time.")
	duplicateMeter   = malformed("usage_report_duplicate_meter", "claude /usage reported the same limit twice.")
)

func malformed(code, message string) failure {
	return failure{
		state:   contracts.ProviderMalformedResponse,
		code:    code,
		phase:   contracts.ErrorParse,
		message: message,
	}
}

func discoveryFailure(err error) failure {
	if errors.Is(err, command.ErrNotFound) {
		return commandNotFound
	}
	return versionUnreadable
}
