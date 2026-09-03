// Byte-safe transport helpers for the manager's tree and editor.
//
// D5 is "bytes in, bytes out": an artifact's content must survive Open and
// Save unchanged, including bytes that are not valid UTF-8 text (a stray
// Latin-1 byte in a hand-edited hook script, say). Go's encoding/json — the
// encoder Wails uses on both sides of a bound-method call — marshals a
// []byte field as a base64 string automatically (see internal/manager's
// package doc), so every byte survives the JSON wire itself regardless of
// what it is. That part is exact and unconditional.
//
// # Where the guarantee actually ends
//
// A <textarea> only ever holds a JS (UTF-16) string, so bytes still have to
// become text to be edited, and back again to be saved. For bytes that ARE
// valid UTF-8 — every real artifact in the bundle today — TextDecoder and
// TextEncoder are exact inverses, and this project's tests (bytes_test.go's
// "awkward bytes" fixture: CRLF, a BOM, a NUL, non-ASCII, no trailing
// newline) confirm the round trip through base64 + UTF-8 is unchanged.
//
// For bytes that are NOT valid UTF-8, decodeText below refuses rather than
// silently substituting: a lenient decoder (TextDecoder's default,
// {fatal: false}) replaces an invalid sequence with U+FFFD, and re-encoding
// that does not reproduce the original bytes — a real byte 0x80 becomes the
// three bytes 0xEF 0xBF 0xBD on save, silently, which is exactly the
// corruption D5 exists to rule out. "Every artifact in the bundle today is
// valid UTF-8" describes today's data, not a property of the format Tachyon
// promises to round-trip, so this module does not lean on it: decodeText
// throws on invalid UTF-8, and the caller (Manager.jsx) turns that into "this
// file can't be opened as text" rather than opening a textarea that would
// corrupt it on the next save.

export function bytesToBase64(bytes) {
  let binary = "";
  const chunk = 0x8000; // avoid a call-stack blowup on String.fromCharCode.apply for a large file
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

export function base64ToBytes(b64) {
  const binary = atob(b64 ?? "");
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

// fatal: true is the whole point — see the module doc. A decode of invalid
// UTF-8 throws a TypeError rather than silently emitting U+FFFD.
const decoder = new TextDecoder("utf-8", { fatal: true });
const encoder = new TextEncoder();

// decodeText throws if bytes is not valid UTF-8. Callers must not swallow
// that: it means this content cannot be safely round-tripped through a text
// editor and must be refused, not opened.
export function decodeText(bytes) {
  return decoder.decode(bytes);
}

export function encodeText(text) {
  return encoder.encode(text);
}

// base64ToText / textToBase64 compose the two conversions above for the
// common case: a Content.bytes field on the way in, a Save argument on the
// way out. base64ToText propagates decodeText's throw on invalid UTF-8.
export function base64ToText(b64) {
  return decodeText(base64ToBytes(b64));
}

export function textToBase64(text) {
  return bytesToBase64(encodeText(text));
}
