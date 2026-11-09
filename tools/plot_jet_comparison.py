#!/usr/bin/env python3
"""Plot measured audio with detector overlays (numpy, scipy, matplotlib).

Run after the standard workbench comparison and just jet-analyze for these
two cases. This is a figure generator, not an alternative detection pipeline.
"""
import json
from pathlib import Path
import matplotlib
matplotlib.use('Agg')
import matplotlib.pyplot as plt
import numpy as np
from scipy.io import wavfile
from scipy.signal import stft


def main():
    cases = [
        ('a320', 'AeroSonicDB A320', 'samples/public/aerosonic-ypad0523/7C7CD0_2022-12-08_12-29-45_0_0.wav', 1000),
        ('phenom-r20', 'Pathak Phenom 300 (R20)', 'samples/public/pathak-2026/N394FX_R20/audio.wav', 1500),
    ]
    fig, axes = plt.subplots(2, 2, figsize=(15, 8), layout='constrained')
    for row, (ident, title, audio, upper) in enumerate(cases):
        rate, pcm = wavfile.read(audio)
        if pcm.ndim == 2:
            pcm = pcm[:, 0]
        window = int(rate * .5)
        hz, times, z = stft(pcm.astype(float), rate, nperseg=window,
                            noverlap=window - window // 8, boundary=None, padded=False)
        db = 20 * np.log10(np.maximum(abs(z), 1e-12))
        band = (hz >= 100) & (hz <= upper)
        floor, ceiling = np.percentile(db[band], [35, 99.8])
        for col, method in enumerate(['standard 2/4 s', 'experimental 0.5/1 s']):
            ax = axes[row, col]
            ax.pcolormesh(times, hz[band], db[band], shading='auto', cmap='magma', vmin=floor, vmax=ceiling)
            name = Path(f'work/jet-reference-baseline/{ident}/production-2+4s-detections.json') if col == 0 else Path(f'work/jet-reference-results/{ident}/detection.json')
            data = json.loads(name.read_text())
            for event in data['events']:
                if event['acoustic_evidence']['level'] == 'strong':
                    ax.plot([p['time_s'] for p in event['ridge']], [p['hz'] for p in event['ridge']], color='cyan', lw=1.1)
            labels = json.loads(Path('references/provenance/aerosonic-a320-anchors.json' if row == 0 else 'references/provenance/phenom-anchors.json').read_text())
            if isinstance(labels, dict):
                labels = labels['anchors']
            ax.scatter([p['time_s'] for p in labels], [p['hz'] for p in labels], s=8, color='yellow', marker='+')
            ax.set(title=f'{title} — {method}', ylim=(100, upper), xlabel='Audio time (s)', ylabel='Hz')
    fig.suptitle('Same measured audio in each column • cyan: strong detected ridges • yellow: evaluation labels\nDevelopment cases, not GPS validation; unplotted/review tones are not claimed absent', fontsize=12)
    dest = Path('work/jet-reference-results/comparison.png')
    dest.parent.mkdir(parents=True, exist_ok=True)
    fig.savefig(dest, dpi=140)
    print(dest)


if __name__ == '__main__':
    main()
