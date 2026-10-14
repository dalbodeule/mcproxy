#!/usr/bin/env sh

set -eu

scanner="${IMAGE_SCANNER:-trivy}"
severity="${IMAGE_SCAN_SEVERITY:-HIGH,CRITICAL}"

if [ "$#" -eq 0 ]; then
	echo "usage: $0 IMAGE [IMAGE ...]" >&2
	exit 2
fi

case "$scanner" in
	trivy)
		command_name=trivy
		;;
	grype)
		command_name=grype
		case "$severity" in
			*HIGH*) grype_fail_on=high ;;
			*CRITICAL*) grype_fail_on=critical ;;
			*) echo "Grype requires IMAGE_SCAN_SEVERITY to include HIGH or CRITICAL" >&2; exit 2 ;;
		esac
		;;
	*)
		echo "unsupported image scanner '$scanner'; supported values: trivy, grype" >&2
		exit 2
		;;
esac

if ! command -v "$command_name" >/dev/null 2>&1; then
	echo "image scanner '$scanner' is not installed; install it before scanning" >&2
	exit 127
fi

for image in "$@"; do
	case "$scanner" in
		trivy) trivy image --exit-code 1 --severity "$severity" --ignore-unfixed "$image" ;;
		grype) grype "$image" --only-fixed --fail-on "$grype_fail_on" ;;
	esac
done
