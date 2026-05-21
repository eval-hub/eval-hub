#!/usr/bin/env bash
# Verifies every reviewer requested on the PR (users and teams) has approved.
# Uses only GitHub PR review-request data, not CODEOWNERS or repository owner files.
set -euo pipefail

pr_number="${PR_NUMBER:-${1:-}}"
repo="${REPOSITORY:-${2:-}}"

if [[ -z "$pr_number" || -z "$repo" ]]; then
	echo "PR_NUMBER and REPOSITORY are required" >&2
	exit 2
fi

owner="${repo%%/*}"

required_label="${REQUIRED_LABEL:-ready-for-review}"
if ! gh pr view "$pr_number" --repo "$repo" --json labels -q '.labels[].name' |
	grep -qx "$required_label"; then
	echo "PR #${pr_number} does not have label ${required_label}; skipping approval check."
	exit 0
fi

latest_review_states() {
	gh api "repos/${repo}/pulls/${pr_number}/reviews" --paginate |
		jq -s 'flatten
      | group_by(.user.login)
      | map(max_by(.submitted_at))
      | map({key: .user.login, value: .state})
      | from_entries'
}

check_user() {
	local login="$1"
	local state
	state=$(jq -r --arg u "$login" '.[$u] // "NONE"' <<<"$states")
	if [[ "$state" != "APPROVED" ]]; then
		if [[ "$state" == "NONE" ]]; then
			echo "- @$login (requested reviewer): no review submitted yet"
		else
			echo "- @$login (requested reviewer): latest review is ${state}, approval required"
		fi
		return 1
	fi
	return 0
}

check_team() {
	local slug="$1"
	local members
	if ! members=$(gh api "orgs/${owner}/teams/${slug}/members" --paginate -q '.[].login' 2>/dev/null); then
		echo "- team @${owner}/${slug}: could not list members (workflow may need members: read permission)" >&2
		return 1
	fi
	if [[ -z "$members" ]]; then
		echo "- team @${owner}/${slug}: no members found"
		return 1
	fi
	local login state
	while read -r login; do
		[[ -z "$login" ]] && continue
		state=$(jq -r --arg u "$login" '.[$u] // "NONE"' <<<"$states")
		if [[ "$state" == "APPROVED" ]]; then
			return 0
		fi
	done <<<"$members"
	echo "- team @${owner}/${slug}: no member has submitted an approval on this PR"
	return 1
}

states=$(latest_review_states)
requested_json=$(gh api "repos/${repo}/pulls/${pr_number}/requested_reviewers")

user_count=$(jq '.users | length' <<<"$requested_json")
team_count=$(jq '.teams | length' <<<"$requested_json")

if [[ "$user_count" -eq 0 && "$team_count" -eq 0 ]]; then
	echo "No requested reviewers on PR #${pr_number}; nothing to verify."
	exit 0
fi

echo "Checking ${user_count} requested user(s) and ${team_count} requested team(s) on PR #${pr_number}..."

failures=0
while read -r login; do
	[[ -z "$login" ]] && continue
	if ! check_user "$login"; then
		failures=$((failures + 1))
	fi
done < <(jq -r '.users[].login' <<<"$requested_json")

while read -r slug; do
	[[ -z "$slug" ]] && continue
	if ! check_team "$slug"; then
		failures=$((failures + 1))
	fi
done < <(jq -r '.teams[].slug' <<<"$requested_json")

if [[ "$failures" -gt 0 ]]; then
	echo "::error::${failures} requested reviewer(s) have not approved PR #${pr_number}"
	exit 1
fi

echo "All requested reviewers have approved PR #${pr_number}."
exit 0
