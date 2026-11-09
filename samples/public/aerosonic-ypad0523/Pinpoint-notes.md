# Pinpoint development subset

These original WAVs, CSVs and README come from **AeroSonicDB YPAD-0523 v1.1.2**,
Blake Downward, DOI [10.5281/zenodo.10215080](https://zenodo.org/records/10215080).
License: **CC BY-NC 4.0**. Also cite
[Downward and Nordby (2023)](https://arxiv.org/abs/2311.06368).
The upstream README is preserved separately and unchanged.

Eight training clips were selected before inspection: first chronological
recording of at least 45 seconds for each of A320/B738/E190/A333/B77W, plus the
first background recording at each of three sites. Selection, original metadata,
ZIP member CRCs and WAV SHA-256 hashes are pinned in
`references/provenance/aerosonic-selection.json`.

The archive was fetched by HTTP ranges, not downloaded in full. Member CRC and
pinned SHA-256 are verified; no whole-archive MD5 verification is asserted.
Use `just jet-reference-fetch` to restore missing files.

Public coordinates deliberately refer to nearby public places, not the precise
microphone locations. The upstream `locations.json` lacks its final closing
brace and spells latitude `lattitude`; those verified source bytes are preserved.
One background filename disagrees with its CSV site ID; neither is silently
corrected. File times are not assumed to be UTC.

See [jet validation](../../../docs/research/jet-validation.md) for current
recognition results and limitations. Aircraft-present clips without annotated
frequency paths are observations, not successful curve-recognition tests.
