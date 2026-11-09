"""Capture aircraft visible in anonymous FR24 playback for local research.

Uses a normal Firefox session and the public map's own playback actions. No
login, tokens, cookies, subscription settings, or access checks are modified.
The frontend state interface is undocumented and may change. This is a sparse
observation inventory, not a complete flight archive or a geometric-altitude
source. Install Selenium and Firefox/geckodriver separately if unavailable.
"""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import time

from selenium import webdriver
from selenium.webdriver.firefox.options import Options
from selenium.webdriver.support.ui import WebDriverWait

STORE = """const e=[...document.querySelectorAll('*')].find(e=>e.__vue_app__);
const pin=e?.__vue_app__.config.globalProperties.$pinia;
const a=pin?._s.get('aircraft'), b=pin?._s.get('playback');"""
FIELDS = ["id", "callsign", "icao", "latitude", "longitude", "altitude",
          "speed", "track", "onGround", "timestamp", "timestampMs", "dataSource"]


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(path.suffix + ".partial")
    temp.write_text(json.dumps(value, indent=2))
    temp.replace(path)


def run():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, type=Path)
    parser.add_argument("--out", type=Path, default=Path("work/flight-history-review"))
    parser.add_argument("--step-seconds", type=int, default=120)
    parser.add_argument("--recording", help="Optional exact recording ID")
    args = parser.parse_args()
    if args.step_seconds < 30:
        parser.error("--step-seconds must be at least 30")
    raw = args.config.read_bytes()
    config = json.loads(raw)
    options = Options()
    options.add_argument("-headless")
    driver = webdriver.Firefox(options=options)
    driver.set_window_size(1600, 1000)
    driver.set_page_load_timeout(45)
    config_hash = hashlib.sha256(raw).hexdigest()
    capture_hash = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    loaded = False
    try:
        for recording in config["recordings"]:
            if args.recording and recording["id"] != args.recording:
                continue
            start = dt.datetime.fromisoformat(recording["start"]).astimezone(dt.timezone.utc)
            first = int(start.timestamp()) // 60 * 60
            end = int(start.timestamp() + recording["duration_s"])
            folder = args.out / recording["id"] / "fr24"
            url = (f"https://www.flightradar24.com/{start:%Y-%m-%d}/{start:%H:%M}"
                   f"/1x/{config['lat']},{config['lon']}/13")
            # Keep one map session when changing recording dates, as the site's
            # date picker does. Repeated full loads can interrupt map startup.
            if not loaded:
                driver.get(url)
                try:
                    WebDriverWait(driver, 40).until(lambda d: d.execute_script(
                        STORE + "return !!a && !!b && Object.keys(a.aircraftMap).length>0;"))
                except Exception:
                    save(args.out / "browser-load-failure.json", dict(url=url,
                         title=driver.title, text=driver.find_element("tag name", "body").text[:4000]))
                    raise
                loaded = True
            # Pause public playback, then use the same two timestamp setters as
            # the site's timeline. All requests remain in the free date range.
            driver.execute_script(STORE + "b.setPlaying(false);")
            records = []
            for index, stamp in enumerate(range(first, end + 1, args.step_seconds)):
                dest = folder / f"{stamp}.json"
                if dest.exists():
                    old = json.loads(dest.read_text())
                    if old.get("config_sha256") == config_hash and old.get("status") == "captured":
                        records.append({"file": str(dest), "status": "captured", "reused": True})
                        continue
                driver.execute_script(STORE + "b.setCurrentTimestamp(arguments[0]*1000);b.setTimestamp(arguments[0]);", stamp)
                status = "captured"
                try:
                    WebDriverWait(driver, 15).until(lambda d: d.execute_script(
                        STORE + "return b.timestamp===arguments[0] && Object.values(a.aircraftMap).some(v=>Math.abs(v.timestamp-arguments[0])<=30);", stamp))
                except Exception:
                    status = "unverified_response_time"
                data = driver.execute_script(STORE + """const fields=arguments[0];
return {playback_timestamp_s:b.timestamp,display_timestamp_ms:b.currentTimestamp,
aircraft:Object.values(a.aircraftMap).map(v=>Object.fromEntries(fields.filter(k=>v[k]!==undefined).map(k=>[k,v[k]])))};""", FIELDS)
                data.update(status=status, requested_timestamp_s=stamp,
                            captured_utc=dt.datetime.now(dt.timezone.utc).isoformat(),
                            config_sha256=config_hash, capture_code_sha256=capture_hash,
                            provider="Flightradar24 public website playback",
                            url=url, altitude_basis="Unverified map altitude; do not use as ellipsoidal height",
                            note="Sparse visible-map observation. Aircraft timestamp is historical; global server/data timestamp may be live. Receiver position was supplied.")
                save(dest, data)
                records.append({"file": str(dest), "status": status, "aircraft": len(data["aircraft"])})
                if index % 10 == 0 or status != "captured":
                    print(recording["id"], index, dt.datetime.fromtimestamp(stamp, dt.timezone.utc).isoformat(), status, len(data["aircraft"]), flush=True)
                time.sleep(2)  # Bound the request rate during sparse review.
            save(folder / "manifest.json", dict(recording=recording, config_sha256=config_hash,
                 capture_code_sha256=capture_hash,
                 step_seconds=args.step_seconds, url=url, snapshots=records,
                 note="Anonymous website observations, not a licensed complete archive. No automatic aircraft sound labels."))
            print(recording["id"], "complete", len(records), flush=True)
    finally:
        driver.quit()


if __name__ == "__main__":
    run()
