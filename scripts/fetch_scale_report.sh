#!/usr/bin/env bash
# Builds the smart scale report and prints the path to report.json on stdout.
# Everything else goes to stderr. The exit code is the one of scale-report:
# 0 report is ready, 2 profile error (PROFILE_* env), 3 no valid measurements,
# 1 file error; any other code means the download failed.
#
# Needs SCALE_TOKEN with read access to danilpapa/ScaleData and danilpapa/ScalePipeline.
# For a local run, SCALE_DATA_DIR and SCALE_REPORT_BIN replace the clone and the download.
#
# The token never goes into a URL or a command line: git reads it through a
# credential helper and curl through a config on stdin, so it cannot leak into
# error messages, .git/config or the process list.
set -euo pipefail

DATA_REPO=danilpapa/ScaleData
CODE_REPO=danilpapa/ScalePipeline
ASSET=scale-report-x86_64-linux

fail() {
	echo "scale: $1" >&2
	exit "${2:-1}"
}

WORK=$(mktemp -d)

if [[ -n "${SCALE_DATA_DIR:-}" ]]; then
	DATA="$SCALE_DATA_DIR"
else
	[[ -n "${SCALE_TOKEN:-}" ]] || fail "SCALE_TOKEN is not set" 10
	DATA="$WORK/ScaleData"
	# Single quotes: the helper expands $SCALE_TOKEN when git calls it.
	GIT_TERMINAL_PROMPT=0 git -c credential.helper= \
		-c credential.helper='!f() { echo username=x-access-token; echo "password=${SCALE_TOKEN}"; }; f' \
		clone --quiet --depth 1 "https://github.com/$DATA_REPO" "$DATA" >&2 ||
		fail "cannot clone $DATA_REPO: check SCALE_TOKEN and that github.com is allowed" 11
fi

if [[ -n "${SCALE_REPORT_BIN:-}" ]]; then
	BIN="$SCALE_REPORT_BIN"
else
	BIN="$WORK/scale-report"

	# The release is public, so the direct link works without a token. It goes through
	# github.com, not api.github.com: in a Claude cloud session the GitHub proxy answers
	# 403 on api.github.com for repositories that are not added to the session.
	download_direct() {
		curl -sS --fail --location --max-time 120 -o "$BIN" \
			"https://github.com/$CODE_REPO/releases/latest/download/$ASSET"
	}

	download_gh() {
		command -v gh >/dev/null 2>&1 && [[ -n "${SCALE_TOKEN:-}" ]] &&
			GH_TOKEN="$SCALE_TOKEN" gh release download -R "$CODE_REPO" -p "$ASSET" -O "$BIN" --clobber
	}

	download_api() {
		[[ -n "${SCALE_TOKEN:-}" ]] || return 1
		auth() { printf 'header = "Authorization: Bearer %s"\n' "$SCALE_TOKEN"; }
		local release asset_url
		release=$(auth | curl -sS --fail --max-time 30 -K - \
			-H "Accept: application/vnd.github+json" \
			"https://api.github.com/repos/$CODE_REPO/releases/latest") || return 1
		if command -v jq >/dev/null 2>&1; then
			asset_url=$(jq -r --arg name "$ASSET" '.assets[] | select(.name == $name) | .url' <<<"$release")
		else
			asset_url=$(python3 -c 'import json, sys; print(next((a["url"] for a in json.load(sys.stdin)["assets"] if a["name"] == sys.argv[1]), ""))' "$ASSET" <<<"$release")
		fi
		[[ -n "$asset_url" ]] || return 1
		# curl drops the Authorization header on the redirect to release-assets.githubusercontent.com.
		auth | curl -sS --fail --location --max-time 120 -K - \
			-H "Accept: application/octet-stream" -o "$BIN" "$asset_url"
	}

	download_direct >&2 || download_gh >&2 || download_api >&2 ||
		fail "cannot download $ASSET from $CODE_REPO: allow github.com and release-assets.githubusercontent.com, or add $CODE_REPO to the session" 12
	chmod +x "$BIN"
fi

# scale-report writes nothing useful to stdout, so it goes to stderr to keep stdout for the path.
status=0
"$BIN" run --data "$DATA" --out "$WORK/out" >&2 || status=$?
case $status in
0) ;;
2) fail "profile error: check PROFILE_SEX, PROFILE_BIRTH_DATE, PROFILE_HEIGHT_CM, PROFILE_GOAL" 2 ;;
3) fail "no valid measurements in $DATA_REPO" 3 ;;
*) fail "scale-report failed with code $status" "$status" ;;
esac

[[ -s "$WORK/out/report.json" ]] || fail "scale-report wrote no report.json" 1
echo "$WORK/out/report.json"
