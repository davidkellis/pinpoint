# DLR A320 RECORD 139

Source: [Schumacher and Siller, DLR, RECORD 139](https://zenodo.org/records/20494053).
License: CC BY 4.0. Cite DOI **10.5281/zenodo.20494053**.

Three channels were exported from the calibrated pressure matrix in `REC139.h5`
as float32 WAV at 48,192 Hz, without amplification or other processing. They
are one seven-second flyover of an Airbus A320-232, not three independent tests.
Selection, exact local coordinates, hashes and the original trajectory values
are in [geometry.json](geometry.json). The GPS origin is unknown.

Original HDF5 cache: `work/jet-reference-source/20494053/REC139.h5`.
Verified upstream MD5: `1fb08d67965550d26d5b35abaf71f3f3`.
Pinned metadata: `references/provenance/zenodo-20494053.json`.
Restore missing audio using `tools/fetch_jet_reference.py` with h5py/scipy.

The center channel currently produces no usable curve. See
[jet validation](../../../docs/research/jet-validation.md); this is not a
successful GPS localization benchmark.
