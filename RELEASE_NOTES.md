## Fixed

- **Poll loop no longer burns API requests after auth expires.** When the API rejected the cached token with 401 and the keychain still held a stale token, `authStatus` could remain `ok`, so each wake-up hit the API four times before a 429 forced a 1h backoff. The provider is now force-marked expired on any 401 from the usage API, so subsequent polls skip the API entirely until credentials are refreshed externally.
