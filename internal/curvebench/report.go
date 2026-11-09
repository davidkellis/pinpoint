package curvebench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strings"

	"pinpoint/internal/acoustic"
)

type Grid struct {
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	LowHz   float64 `json:"low_hz"`
	HighHz  float64 `json:"high_hz"`
	Floor   float64 `json:"floor"`
	Ceiling float64 `json:"ceiling"`
	Pixels  []byte  `json:"pixels,omitempty"`
}

func makeGrid(s *acoustic.Spectrum, raw bool) Grid {
	df := float64(s.SampleRate) / float64(s.Window)
	g := Grid{Width: min(800, len(s.Rows)), Height: min(900, len(s.Rows[0])), Start: s.Time(0) - s.Step()/2, End: s.Time(len(s.Rows)-1) + s.Step()/2, LowHz: s.Hz(0) - df/2, HighHz: s.Hz(len(s.Rows[0])-1) + df/2, Floor: -35, Ceiling: 45}
	if raw {
		g.Floor, g.Ceiling = -120, 80
	}
	g.Pixels = make([]byte, g.Width*g.Height)
	for x := 0; x < g.Width; x++ {
		a, b := x*len(s.Rows)/g.Width, (x+1)*len(s.Rows)/g.Width
		for y := 0; y < g.Height; y++ {
			lo, hi := y*len(s.Rows[0])/g.Height, (y+1)*len(s.Rows[0])/g.Height
			v := g.Floor
			for i := a; i < b; i++ {
				for k := lo; k < hi; k++ {
					v = math.Max(v, float64(s.Rows[i][k]))
				}
			}
			g.Pixels[y*g.Width+x] = uint8(math.Round(math.Max(0, math.Min(255, (v-g.Floor)/(g.Ceiling-g.Floor)*255))))
		}
	}
	return g
}

const style = `<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{background:#122127;color:#dbe6e8;font:16px system-ui;margin:28px;line-height:1.5}a{color:#88ddcf}h1{font-size:25px}table{border-collapse:collapse;width:100%;font-size:13px}td,th{border:1px solid #3b515a;padding:8px;text-align:left}th{background:#20363e}small,.muted{color:#a9bec5}button,input,select{font:inherit;color:inherit;background:#20363e;border:1px solid #627f89;padding:7px;margin:4px}input[type=number]{width:90px}label{display:inline-block;margin-right:12px}canvas{width:100%;max-width:1500px;background:#070b12;display:block}pre{white-space:pre-wrap}.good{color:#9be1af}.warn{color:#f2be6b}.scroll{overflow:auto}nav{margin:15px 0}</style>`

func writeCase(dir string, r CaseReport) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	page := `<!doctype html><title>Doppler experiment</title>` + style + `<a href="../index.html">← All experiments</a><h1>` + html.EscapeString(r.Input.Label) + `</h1><p>Experimental sound evidence. No aircraft identity or location is asserted. Views use measured spectral energy; harmonic pooling is an evidence projection, not a raw spectrogram.</p><nav><label>Method <select id="method"></select></label><label>Lower Hz <input id="low" type="number" value="40"></label><label>Upper Hz <input id="high" type="number" value="800"></label><label>Display floor dB <input id="floor" type="number" value="-5"></label><label>Display ceiling dB <input id="ceiling" type="number" value="25"></label><label><input id="curves" type="checkbox"> Candidate ridges</label><label><input id="truth" type="checkbox"> Known test trace</label></nav><p id="stats"></p><canvas id="plot" width="1400" height="610"></canvas><p id="cursor"></p><p class="muted">Horizontal axis: time in the original recording; vertical axis: Hz. Raw: unnormalized FFT log power (not calibrated sound pressure). Other methods: relative spectral contrast. Display is max-pooled and quantized; detection and evaluation use full-precision measurements. Cyan paths are proposals, yellow paths are test labels, neither changes the underlying image. Showing a test label does not make it a detected curve.</p><details><summary>Input and interpretation</summary><pre id="input"></pre></details><script>const data=` + string(b) + `;</script>` + script
	return os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0600)
}

const script = `<script>
const $=id=>document.getElementById(id),menu=$('method'),canvas=$('plot'),ctx=canvas.getContext('2d');
const raw={method:'raw-2s',plot:data.raw,events:[]};const results=[raw,...data.results];let decoded=new Map();
for(const [i,r]of results.entries()){const o=document.createElement('option');o.value=i;o.textContent=r.method;menu.append(o);}menu.value=results.length-1;
$('input').textContent=JSON.stringify({input:data.input,sha256:data.audio_sha256,note:'Clip whitening differs from whole-recording whitening. Window comparisons change both temporal and frequency resolution. Timings exclude shared 2/4-second FFT setup and are not performance benchmarks. Jet injections are stable tonal simulations, not labeled real airliner recordings.'},null,2);
const colours=[[3,3,8],[16,35,63],[38,37,112],[123,40,138],[195,52,163],[235,96,74],[248,185,67],[255,241,170]];
function draw(){const r=results[Number(menu.value)],g=r.plot;if(!decoded.has(r.method))decoded.set(r.method,Uint8Array.from(atob(g.pixels),c=>c.charCodeAt(0)));const p=decoded.get(r.method);const lo=Math.max(g.low_hz,Number($('low').value)),hi=Math.min(g.high_hz,Number($('high').value)),floor=Number($('floor').value),ceiling=Number($('ceiling').value);if(!(lo<hi&&floor<ceiling))return;
const left=65,top=20,w=canvas.width-left-20,h=canvas.height-top-55;ctx.clearRect(0,0,canvas.width,canvas.height);const im=ctx.createImageData(w,h);
for(let y=0;y<h;y++){const freq=hi-(y+.5)/h*(hi-lo),row=Math.min(g.height-1,Math.max(0,Math.floor((freq-g.low_hz)/(g.high_hz-g.low_hz)*g.height)));for(let x=0;x<w;x++){const col=Math.min(g.width-1,Math.floor(x/w*g.width));const v=g.floor+p[row*g.width+col]/255*(g.ceiling-g.floor),q=Math.max(0,Math.min(6.999,(v-floor)/(ceiling-floor)*7)),a=Math.floor(q),u=q-a,o=(y*w+x)*4;for(let k=0;k<3;k++)im.data[o+k]=colours[a][k]*(1-u)+colours[a+1][k]*u;im.data[o+3]=255;}}ctx.putImageData(im,left,top);ctx.fillStyle='#c6d8df';ctx.font='14px system-ui';ctx.strokeStyle='#778e9550';
for(let k=0;k<=6;k++){const x=left+w*k/6,y=top+h*k/6;ctx.textAlign=k===0?'left':k===6?'right':'center';ctx.fillText((g.start+(g.end-g.start)*k/6+data.input.start_s).toFixed(1)+'s',x,top+h+25);ctx.textAlign='left';ctx.fillText((hi-(hi-lo)*k/6).toFixed(0),8,y+5);ctx.beginPath();ctx.moveTo(x,top);ctx.lineTo(x,top+h);ctx.stroke();}ctx.fillText('Hz',8,14);
function line(points,colour,dots=false){ctx.save();ctx.beginPath();ctx.rect(left,top,w,h);ctx.clip();ctx.strokeStyle=colour;ctx.lineWidth=2;ctx.beginPath();let prev=-1e9;for(const p of points){const x=left+(p.time_s-g.start)/(g.end-g.start)*w,y=top+(hi-p.hz)/(hi-lo)*h;if(p.time_s-prev>4)ctx.moveTo(x,y);else ctx.lineTo(x,y);prev=p.time_s;}ctx.stroke();if(dots){ctx.fillStyle=colour;for(const p of points){ctx.beginPath();ctx.arc(left+(p.time_s-g.start)/(g.end-g.start)*w,top+(hi-p.hz)/(hi-lo)*h,3,0,2*Math.PI);ctx.fill();}}ctx.restore();}
if($('curves').checked)for(const e of r.events||[])line(e.ridge||[],'#64e9fc');if($('truth').checked)line(data.input.anchors||[],'#ffe380',true);
const m=r.known_curve_match;$('stats').textContent=r.method==='raw-2s'?'Raw measurements, no detector run on this display.':r.strong+' strong acoustic candidates; '+r.review+' need review; '+r.fragments+' fragments.'+(m?' Known-path recovery: '+(m.recovered?'yes':'no')+' ('+(m.anchor_hit_fraction*100).toFixed(0)+'% of anchors; '+m.evidence_level+').':' No timestamp labels: recall and aircraft identity cannot be determined.');}
menu.onchange=()=>{const raw=results[Number(menu.value)].method==='raw-2s';$('floor').value=raw?-55:-5;$('ceiling').value=raw?15:25;draw();};for(const id of ['low','high','floor','ceiling','curves','truth'])$(id).oninput=draw;draw();
canvas.onmousemove=e=>{const r=canvas.getBoundingClientRect(),g=results[Number(menu.value)].plot,x=(e.clientX-r.left)*canvas.width/r.width,y=(e.clientY-r.top)*canvas.height/r.height,lo=Math.max(g.low_hz,Number($('low').value)),hi=Math.min(g.high_hz,Number($('high').value));$('cursor').textContent=(g.start+(x-65)/(canvas.width-85)*(g.end-g.start)+data.input.start_s).toFixed(2)+' s · '+(hi-(y-20)/(canvas.height-75)*(hi-lo)).toFixed(2)+' Hz';};
</script>`

func writeIndex(out string, s Summary) error {
	var b strings.Builder
	b.WriteString(`<!doctype html><title>Doppler extraction experiments</title>` + style + `<h1>Doppler extraction experiments</h1><p>Compare measured spectral representations and the existing detector on labeled reference curves, controlled injections, non-Doppler controls, and unlabeled walks. A strong acoustic curve is not a confirmed aircraft.</p><p>Each input opens an interactive comparison with frequency and contrast controls, optional candidate overlays, and separately labeled test traces. Production settings are unchanged.</p><p class="muted">Known-path recovery requires at least 80% of labeled time-frequency anchors within max(3 Hz, 1.5%) after one fixed harmonic scaling. “Curve” means a recovered path failed the strong-evidence gate. S/R are strong/review counts, not aircraft counts. Noise and disrupted-signal controls measure spurious detections only in this small experiment, not a population false-alarm rate. Synthetic jets do not establish real-jet recall.</p><div class="scroll"><table><tr><th>Input</th><th>Kind</th>`)
	for _, m := range s.Methods {
		fmt.Fprintf(&b, "<th>%s</th>", html.EscapeString(m))
	}
	b.WriteString("</tr>")
	f, err := os.Create(filepath.Join(out, "metrics.csv"))
	if err != nil {
		return err
	}
	csvOut := csv.NewWriter(f)
	csvOut.Write([]string{"case", "kind", "method", "strong", "review", "fragments", "known_anchor_hit_fraction", "known_path_recovered", "known_path_strong", "milliseconds_excluding_shared_fft"})
	for _, c := range s.Reports {
		if c.Input.ID == "" {
			continue
		}
		fmt.Fprintf(&b, `<tr><td><a href="%s/index.html">%s</a></td><td>%s</td>`, c.Input.ID, html.EscapeString(c.Input.Label), html.EscapeString(c.Input.Kind))
		for _, r := range c.Results {
			text := fmt.Sprintf("%d S / %d R", r.Strong, r.Review)
			hit, recovered, strong := "", "", ""
			if m := r.Match; m != nil {
				hit = fmt.Sprint(m.HitFraction)
				recovered = fmt.Sprint(m.Recovered)
				strong = fmt.Sprint(m.Strong)
				status := "miss"
				if m.Recovered {
					status = "curve"
				}
				if m.Strong {
					status = "strong curve"
				}
				text = fmt.Sprintf("%s · %.0f%%", status, m.HitFraction*100)
			}
			fmt.Fprintf(&b, "<td>%s</td>", text)
			csvOut.Write([]string{c.Input.ID, c.Input.Kind, r.Method, fmt.Sprint(r.Strong), fmt.Sprint(r.Review), fmt.Sprint(r.Fragments), hit, recovered, strong, fmt.Sprint(r.Milliseconds)})
		}
		b.WriteString("</tr>")
	}
	csvOut.Flush()
	err = csvOut.Error()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	b.WriteString(`</table></div><p><a href="metrics.csv">Metrics CSV</a> · <a href="summary.json">Machine-readable summary</a> · <a href="manifest.json">Run manifest</a></p><p>Reproduce with <code>just experiment --config PATH/TO/manifest.json --out NEW_REPORT_DIRECTORY</code>. Reassignment is frequency-only and uses the Hann-window derivative; time coordinates are not reassigned. Harmonic pooling combines 1×, 2× and 3× contrasts and can suppress single tones. Both remain experimental. These views cannot establish aircraft identity or recording location.</p>`)
	if len(s.Errors) > 0 {
		fmt.Fprintf(&b, "<pre>%s</pre>", html.EscapeString(strings.Join(s.Errors, "\n")))
	}
	return os.WriteFile(filepath.Join(out, "index.html"), []byte(b.String()), 0600)
}

// Rebuild updates the viewer and anchor evaluation without repeating FFTs or
// changing saved detections. It never reads or writes production web jobs.
func Rebuild(out string) error {
	read := func(path string, v any) error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}
	var s Summary
	if err := read(filepath.Join(out, "summary.json"), &s); err != nil {
		return err
	}
	var config Config
	for i := range s.Reports {
		id := s.Reports[i].Input.ID
		if id == "" {
			continue
		}
		// Validate the directory component before reading any case files.
		if err := Validate(Config{Cases: []Case{s.Reports[i].Input}}); err != nil {
			return err
		}
		dir := filepath.Join(out, id)
		var r CaseReport
		if err := read(filepath.Join(dir, "report.json"), &r); err != nil {
			return err
		}
		r.MatchVersion = MatchVersion
		for j := range r.Results {
			v := &r.Results[j]
			if err := Validate(Config{Cases: []Case{r.Input}, Methods: []string{v.Method}}); err != nil {
				return err
			}
			var d acoustic.DetectionReport
			if err := read(filepath.Join(dir, v.Method+"-detections.json"), &d); err != nil {
				return err
			}
			v.Match = match(d.Events, r.Input.Anchors)
		}
		if err := JSON(filepath.Join(dir, "report.json"), r); err != nil {
			return err
		}
		if err := writeCase(dir, r); err != nil {
			return err
		}
		s.Reports[i] = r
		c := r.Input
		if c.Inject != nil {
			c.Anchors = nil
		}
		config.Cases = append(config.Cases, c)
	}
	if len(s.Methods) == 0 && len(s.Reports) > 0 {
		for _, r := range s.Reports[0].Results {
			s.Methods = append(s.Methods, r.Method)
		}
	}
	config.Methods = s.Methods
	if err := JSON(filepath.Join(out, "manifest.json"), config); err != nil {
		return err
	}
	if err := JSON(filepath.Join(out, "summary.json"), compact(s)); err != nil {
		return err
	}
	return writeIndex(out, s)
}
