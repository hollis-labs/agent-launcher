#!/bin/bash
# Build the committed frontend and a signed, launchable macOS application.
# The output path and signing identity are intentionally stable so macOS TCC
# can continue to identify Tachyon after a rebuild.
set -euo pipefail

readonly bundle_id="com.hollislabs.tachyon"
readonly app_name="Tachyon"
readonly identity_name="Tachyon Local Code Signing"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
output_dir="$repo_root/build/bin"
app_path="$output_dir/$app_name.app"
requested_signing_home="${TACHYON_SIGNING_HOME:-$HOME/Library/Application Support/Tachyon/signing}"
signing_home=""
keychain_path=""
password_path=""
certificate_path=""
build_lock_path=""
lock_held=false
package_tmp=""
identity_tmp=""

die() {
	printf 'build-macos: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [[ -n "$package_tmp" && -d "$package_tmp" ]]; then
		/bin/rm -rf -- "$package_tmp"
	fi
	if [[ -n "$identity_tmp" && -d "$identity_tmp" ]]; then
		/bin/rm -rf -- "$identity_tmp"
	fi
	if [[ "$lock_held" == true && -f "$build_lock_path" && "$(<"$build_lock_path")" == "$$" ]]; then
		/bin/rm -f -- "$build_lock_path"
	fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

[[ "$(uname -s)" == "Darwin" ]] || die "macOS is required"
for command in git go npm openssl security codesign sips iconutil plutil shlock; do
	command -v "$command" >/dev/null 2>&1 || die "required command not found: $command"
done

[[ "$requested_signing_home" == /* && "$requested_signing_home" != "/" ]] || die "TACHYON_SIGNING_HOME must be an absolute non-root path"
signing_leaf="$(basename "$requested_signing_home")"
requested_signing_parent="$(dirname "$requested_signing_home")"
mkdir -p "$requested_signing_parent"
signing_parent="$(cd "$requested_signing_parent" && pwd -P)"
signing_home="$signing_parent/$signing_leaf"
keychain_path="$signing_home/Tachyon.keychain-db"
password_path="$signing_home/keychain-password"
certificate_path="$signing_home/Tachyon-Code-Signing.pem"
build_lock_path="$signing_parent/.$signing_leaf.build.lock"

# shlock creates the PID file with an atomic link and replaces locks whose PID
# is no longer alive. The lock covers the complete build, not just first-run
# signer creation: npm, app replacement and every keychain mutation are all
# serialized. EXIT and signal traps remove only the lock owned by this PID.
if ! shlock -f "$build_lock_path" -p "$$"; then
	lock_owner="unknown"
	[[ -f "$build_lock_path" ]] && lock_owner="$(<"$build_lock_path")"
	die "another Tachyon package build (PID $lock_owner) holds $build_lock_path"
fi
lock_held=true

# SIGKILL cannot run traps. Once the lock is acquired, any bootstrap staging
# directory is therefore abandoned and safe to remove; a live creator would
# still own the lock and this process could not reach here.
for stale_bootstrap in "$signing_parent/.$signing_leaf.bootstrap."*; do
	[[ -d "$stale_bootstrap" ]] || continue
	case "$stale_bootstrap" in
	"$signing_parent/.$signing_leaf.bootstrap."*) /bin/rm -rf -- "$stale_bootstrap" ;;
	*) die "refusing to remove unexpected bootstrap path $stale_bootstrap" ;;
	esac
done

create_signing_identity() {
	umask 077
	identity_tmp="$(mktemp -d "$signing_parent/.$signing_leaf.bootstrap.XXXXXX")"
	staged_keychain="$identity_tmp/Tachyon.keychain-db"
	staged_password="$identity_tmp/keychain-password"
	staged_certificate="$identity_tmp/Tachyon-Code-Signing.pem"
	openssl rand -hex 32 >"$staged_password"
	openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 7300 \
		-subj "/CN=$identity_name/O=Hollis Labs/OU=Local Development" \
		-addext "keyUsage=critical,digitalSignature" \
		-addext "extendedKeyUsage=codeSigning" \
		-out "$staged_certificate" -keyout "$identity_tmp/private-key.pem" >/dev/null 2>&1
	openssl pkcs12 -export -out "$identity_tmp/identity.p12" \
		-inkey "$identity_tmp/private-key.pem" -in "$staged_certificate" \
		-passout "file:$staged_password"
	security create-keychain -p "$(<"$staged_password")" "$staged_keychain"
	security unlock-keychain -p "$(<"$staged_password")" "$staged_keychain"
	security set-keychain-settings -lut 21600 "$staged_keychain"
	security import "$identity_tmp/identity.p12" -k "$staged_keychain" \
		-P "$(<"$staged_password")" -T /usr/bin/codesign >/dev/null
	security set-key-partition-list -S apple-tool:,apple:,codesign: -s \
		-k "$(<"$staged_password")" "$staged_keychain" >/dev/null
	/bin/rm -f -- "$identity_tmp/private-key.pem" "$identity_tmp/identity.p12"
	[[ ! -e "$signing_home" ]] || die "signing home appeared while the build lock was held: $signing_home"
	# Publishing one directory rename means interruption exposes either no
	# identity or all three persistent files. Trust/search-list setup below is
	# idempotent, so interruption immediately after this rename is recoverable.
	mv "$identity_tmp" "$signing_home"
	identity_tmp=""
}

artifact_count=0
[[ -f "$keychain_path" ]] && artifact_count=$((artifact_count + 1))
[[ -f "$password_path" ]] && artifact_count=$((artifact_count + 1))
[[ -f "$certificate_path" ]] && artifact_count=$((artifact_count + 1))
if [[ "$artifact_count" -eq 0 ]]; then
	if [[ -d "$signing_home" ]]; then
		/bin/rmdir "$signing_home" 2>/dev/null || die "signing home has no identity files but is not empty: $signing_home"
	elif [[ -e "$signing_home" ]]; then
		die "signing home exists but is not a directory: $signing_home"
	fi
	printf 'Creating the persistent local Tachyon code-signing identity in:\n  %s\n' "$signing_home"
	create_signing_identity
elif [[ "$artifact_count" -ne 3 ]]; then
	die "local signing identity is incomplete at $signing_home; restore the missing file instead of rotating Tachyon's TCC identity"
fi

security unlock-keychain -p "$(<"$password_path")" "$keychain_path"

# codesign does not honor --keychain for a private key unless the keychain is
# also in the user search list. Add this dedicated keychain once, preserving
# every existing entry and its whitespace.
keychain_present=false
keychains=()
while IFS= read -r line; do
	entry="${line#*\"}"
	entry="${entry%\"*}"
	[[ -n "$entry" ]] || continue
	keychains+=("$entry")
	if [[ "$entry" == "$keychain_path" ]]; then
		keychain_present=true
	fi
done < <(security list-keychains -d user)
if [[ "$keychain_present" == false ]]; then
	security list-keychains -d user -s "${keychains[@]}" "$keychain_path"
fi

# A self-signed identity is valid for code signing only after its public
# certificate is trusted for that policy. Restoring missing trust is safe and
# also completes a first build interrupted just after the atomic publish: it
# reuses this certificate and private key rather than rotating TCC identity.
if ! security verify-cert -c "$certificate_path" -p codeSign >/dev/null 2>&1; then
	printf 'Restoring code-signing trust for the existing Tachyon certificate...\n'
	security add-trusted-cert -r trustRoot -p codeSign -k "$keychain_path" "$certificate_path"
fi
security verify-cert -c "$certificate_path" -p codeSign >/dev/null 2>&1 || die "the existing Tachyon certificate could not be trusted for code signing"

identity_hash="$(security find-identity -v -p codesigning "$keychain_path" | awk -v name="\"$identity_name\"" '$0 ~ name { print $2; exit }')"
[[ "$identity_hash" =~ ^[[:xdigit:]]{40}$ ]] || die "could not find the persistent '$identity_name' identity"

previous_requirement=""
if [[ -d "$app_path" ]]; then
	previous_requirement="$(codesign -d -r- "$app_path" 2>&1 | awk '/^designated =>/{print; exit}' || true)"
fi

cd "$repo_root"
printf 'Building frontend...\n'
npm --prefix frontend ci
npm --prefix frontend run build
go run ./cmd/tachyon-packagecheck

mkdir -p "$output_dir"
package_tmp="$(mktemp -d "$output_dir/.tachyon-package.XXXXXX")"
stage_app="$package_tmp/$app_name.app"
mkdir -p "$stage_app/Contents/MacOS" "$stage_app/Contents/Resources"

printf 'Building Wails application...\n'
CGO_CFLAGS="-mmacosx-version-min=12.0" \
CGO_LDFLAGS="-mmacosx-version-min=12.0" \
MACOSX_DEPLOYMENT_TARGET="12.0" \
	go build -tags production -trimpath -ldflags="-s -w -buildid=" \
	-o "$stage_app/Contents/MacOS/$app_name" .

cp packaging/macos/Info.plist "$stage_app/Contents/Info.plist"
plutil -lint "$stage_app/Contents/Info.plist" >/dev/null

iconset="$package_tmp/AppIcon.iconset"
mkdir -p "$iconset"
for spec in "16:16" "16:32" "32:32" "32:64" "128:128" "128:256" "256:256" "256:512" "512:512" "512:1024"; do
	name="${spec%%:*}"
	pixels="${spec##*:}"
	suffix=""
	[[ "$pixels" -eq $((name * 2)) ]] && suffix="@2x"
	sips -z "$pixels" "$pixels" packaging/macos/AppIcon.png \
		--out "$iconset/icon_${name}x${name}${suffix}.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$stage_app/Contents/Resources/AppIcon.icns"

printf 'Signing with persistent identity %s...\n' "$identity_hash"
codesign --force --keychain "$keychain_path" --sign "$identity_hash" \
	--identifier "$bundle_id" --options runtime --timestamp=none "$stage_app"
codesign --verify --deep --strict --verbose=2 "$stage_app"

actual_requirement="$(codesign -d -r- "$stage_app" 2>&1 | awk '/^designated =>/{print; exit}')"
lower_identity="$(printf '%s' "$identity_hash" | tr '[:upper:]' '[:lower:]')"
expected_requirement="designated => identifier \"$bundle_id\" and certificate root = H\"$lower_identity\""
[[ "$actual_requirement" == "$expected_requirement" ]] || die "unexpected designated requirement: $actual_requirement"
if [[ -n "$previous_requirement" && "$actual_requirement" != "$previous_requirement" ]]; then
	die "designated requirement changed across rebuilds; refusing to replace the existing app"
fi

if [[ -e "$app_path" ]]; then
	/bin/rm -rf -- "$app_path"
fi
mv "$stage_app" "$app_path"

printf '\nBuilt %s\n' "$app_path"
printf 'Bundle identifier: %s\n' "$bundle_id"
printf 'Designated requirement: %s\n' "$actual_requirement"
printf 'Launch through LaunchServices with: open "%s"\n' "$app_path"
