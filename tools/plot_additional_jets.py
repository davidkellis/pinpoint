#!/usr/bin/env python3
"""Measured jet spectra, with publisher labels and independently detected ridges.

Requires numpy, scipy and matplotlib. Does not modify audio or run detection.
"""
import argparse
import json
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np
from scipy.io import wavfile
from scipy.signal import stft


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--r21-detection', type=Path, default=Path('work/additional-jets/r21/detection.json'))
    parser.add_argument('--r50-detection', type=Path, default=Path('work/additional-jets/r50/detection.json'))
    parser.add_argument('--out', type=Path, default=Path('work/additional-jets/comparison.png'))
    args = parser.parse_args()
    cases = [('r21', 'N563FX_R21', 'Challenger — N563FX / R21', 1500),
             ('r50', 'N958JX_R50', 'Embraer regional jet — N958JX / R50', 800)]
    fig, axes = plt.subplots(2, 2, figsize=(16, 9), layout='constrained')
    for row, (ident, directory, title, upper) in enumerate(cases):
        rate, pcm = wavfile.read(f'samples/public/pathak-2026/{directory}/audio.wav')
        if pcm.ndim == 2:
            pcm = pcm[:, 0]
        n = int(rate * .5)
        hz, times, z = stft(pcm.astype(float), rate, nperseg=n,
                            noverlap=n - n // 8, boundary=None, padded=False)
        band = (hz >= 60) & (hz <= upper)
        db = 20 * np.log10(np.maximum(abs(z[band]), 1e-12))
        lo, hi = np.percentile(db, [35, 99.8])
        anchors = json.loads(Path(f'references/provenance/pathak-{ident}/anchors.json').read_text())
        detection = json.loads(getattr(args, f'{ident}_detection').read_text())
        for col, ax in enumerate(axes[row]):
            ax.pcolormesh(times, hz[band], db, shading='auto', cmap='magma', vmin=lo, vmax=hi)
            if col:
                ax.scatter([p['time_s'] for p in anchors], [p['hz'] for p in anchors],
                           s=6, marker='+', color='yellow', label='Publisher frequency samples')
                for e in detection['events']:
                    if e['acoustic_evidence']['level'] == 'strong':
                        ax.plot([p['time_s'] for p in e['ridge']], [p['hz'] for p in e['ridge']],
                                color='cyan', lw=1.2)
                ax.legend(loc='upper right', fontsize=8)
            ax.set(title=title + (' — raw measured spectrum' if col == 0 else ' — labels + strong ridges'),
                   xlabel='Time within WAV (s)', ylabel='Frequency (Hz)', ylim=(60, upper))
    fig.suptitle('Additional jet development cases: cyan = strong automatic short-pass ridges; yellow = evaluation labels\nSame measured audio in both columns. No GPS or aircraft identity inferred from this figure.', fontsize=12)
    dest = args.out
    dest.parent.mkdir(parents=True, exist_ok=True)
    fig.savefig(dest, dpi=140)
    print(dest)


if __name__ == '__main__':
    main()
