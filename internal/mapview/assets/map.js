(() => {
  'use strict';
	if(new URLSearchParams(location.search).has('embedded'))document.documentElement.classList.add('embedded');
  const data = JSON.parse(document.getElementById('map-data').textContent);
  const report = data.report;
  const points = report.score_grid || [];
  const $ = id => document.getElementById(id);
  const finite = value => typeof value === 'number' && Number.isFinite(value);
  const format = value => finite(value) ? value.toFixed(2) : '—';
  const signed = value => finite(value) ? `${value >= 0 ? '+' : ''}${value.toFixed(2)} s` : '—';
  const coordinate = p => `${p.lat.toFixed(6)}, ${p.lon.toFixed(6)}`;
  const passed = points.filter(p => p.meets_threshold).length;
  const unscored = points.filter(p => !finite(p.score_db)).length;
  const hasMatch = report.status === 'candidate_matches' && report.candidates.length > 0;
  const single=data.event_count===1, scoreLabel=single?'Single-event match score':'Joint match score';
  $('result-title').textContent=report.ambiguity||single?'Location unresolved':'Candidate joint fits';
  $('result-scope').textContent=single?'This map uses one sound interval. It does not combine the other events or establish a recording location.':`${data.event_count} selected flyovers share one observer position and recording clock. Fits remain unverified.`;
  $('metric').options[0].textContent=scoreLabel;
  $('threshold-label').textContent=single?'Only locations passing this event’s score threshold':'Only locations passing every selected event’s score threshold';
  $('status').textContent = hasMatch ? 'Candidate matches · unverified' : 'No match passed checks';
  $('status').classList.toggle('no-match', !hasMatch);
  $('source').textContent = data.source;
  $('compare-curves').onclick=()=>PinpointEvidence.open(data.evidence||location.pathname.replace(/map\.html$/,'evidence.json')); 
  $('analysis-note').textContent = report.note || '';
  $('ambiguity').hidden = !report.ambiguity;
  $('ambiguity').textContent = report.ambiguity || '';
  if(report.ambiguity)$('status').textContent='Ambiguous candidate matches';
  const evaluated = report.locations_evaluated ?? points.length;
  $('summary').textContent = evaluated === points.length
    ? `${evaluated.toLocaleString()} evaluations · ${passed.toLocaleString()} passing · ${unscored.toLocaleString()} without a complete fit`
    : `${evaluated.toLocaleString()} locations evaluated · ${points.length.toLocaleString()} modeled points plotted · ${passed.toLocaleString()} passing`;
  $('grid-caption').textContent = `${report.coarse_grid_m} m geometry/coarse grid${report.dense_grid_m?` / ${report.dense_grid_m} m dense scoring`:``}${report.refine_grid_m > 0 ? ` / ${(report.refinement_passes||[]).map(p=>p.spacing_m).join(" → ")||report.refine_grid_m} m refinement` : ''} · click a dot to inspect`;
  for (let i = 0; !single && i < data.event_count; i++) {
    const option = document.createElement('option'); option.value = String(i); option.textContent = `Flyover ${i + 1} score at shared fit`; $('metric').append(option);
  }

  const map = L.map('map', {preferCanvas: true, zoomAnimation: false, maxZoom: 21, minZoom: 2});
  const bounds = [[report.bounds.south, report.bounds.west], [report.bounds.north, report.bounds.east]];
  const fit = () => { map.closePopup(); map.fitBounds(bounds, {padding: [45, 45], maxZoom: 17}); };
  fit();
  L.control.scale({imperial: false, position: 'bottomleft'}).addTo(map);
  const context=PinpointMapContext(map,{boundaries:JSON.parse(document.getElementById('borders-data').textContent),selectedState:data.region_mask,onWarning:message=>{$('tile-message').textContent=message;$('tile-message').hidden=!message;}});
  $('basemap').value=context.preferences.base;
  $('state-borders').checked=context.preferences.borders;
  $('road-labels').checked=context.preferences.roads;
  $('road-labels').disabled=context.preferences.base!=='satellite';
  L.rectangle(bounds, {color: '#d9f2ee', weight: 1, dashArray: '5 6', fill: false, interactive: false}).addTo(map);
  $('basemap').onchange=()=>{context.setBase($('basemap').value);$('road-labels').disabled=context.preferences.base!=='satellite';};
  $('state-borders').onchange=()=>context.setBorders($('state-borders').checked);
  $('road-labels').onchange=()=>context.setRoads($('road-labels').checked);

  const flightLayer = L.layerGroup();
  const flightColors = ['#80ebef', '#f7baff', '#f3e090', '#acc2ff'];
  for (const [i, flight] of (data.flight_paths || []).entries()) {
    const label = document.createElement('span'); label.textContent = `Flyover ${flight.event}: ${flight.track_id}`;
    L.polyline(flight.segments, {color: flightColors[i % flightColors.length], weight: 2, opacity: .85}).bindTooltip(label).addTo(flightLayer);
  }
  $('flights').disabled = !(data.flight_paths || []).length;
  $('flights').addEventListener('change', () => { if ($('flights').checked) flightLayer.addTo(map); else map.removeLayer(flightLayer); });

  let selectedRing;
  function inspect(p) {
    $('details').replaceChildren();
    const title = document.createElement('p'); title.className = 'eyebrow'; title.textContent = 'SELECTED LOCATION';
    const dl = document.createElement('dl');
    const row = (name, value) => { const dt = document.createElement('dt'); dt.textContent = name; const dd = document.createElement('dd'); dd.textContent = value; dl.append(dt, dd); };
    row('Coordinates', coordinate(p));
    row(scoreLabel, finite(p.score_db) ? `${format(p.score_db)} dB` : 'No complete fit');
    row('Assessment', !finite(p.score_db) ? 'Insufficient geometry or spectral coverage' : p.meets_threshold ? (single?'This event passes its score threshold':'Selected events pass their score thresholds') : 'Below threshold for one or more events');
    row('Recording clock correction', signed(p.clock_offset_s));
    row('Grid', `${p.stage} · ${p.grid_m} m spacing`);
    (p.event_scores_db || []).forEach((score, i) => row(`Flyover ${i + 1}`, `${format(score)} dB · ${p.track_ids[i]}`));
    $('details').append(title, dl);
    const popup=document.createElement('div');
    const heading=document.createElement('strong');heading.textContent=coordinate(p);
    const value=document.createElement('div');value.textContent=finite(p.score_db)?`${format(p.score_db)} dB ${scoreLabel.toLowerCase()}`:'No complete fit at this location';
    const assessment=document.createElement('div');assessment.textContent=p.meets_threshold?'Passes configured score thresholds; unverified':finite(p.score_db)?'Below the configured event threshold':'Insufficient geometry or spectral coverage';
    popup.append(heading,value,assessment);
    L.popup({maxWidth:280,autoPan:false}).setLatLng([p.lat,p.lon]).setContent(popup).openOn(map);
    if (selectedRing) map.removeLayer(selectedRing);
    selectedRing = L.circleMarker([p.lat, p.lon], {radius: 9, color: 'white', weight: 2, fill: false, interactive: false}).addTo(map);
  }
  if (hasMatch) {
    const best = report.candidates[0];
    $('best').disabled = false; $('best').textContent = coordinate(best);
    $('best-score').textContent = `${format(best.score_db)} dB ${scoreLabel.toLowerCase()}`;
    $('best-clock').textContent = `Recording clock correction ${signed(best.clock_offset_s)}`;
    const target = () => points.find(p => p.meets_threshold && Math.abs(p.lat-best.lat)<1e-8 && Math.abs(p.lon-best.lon)<1e-8);
    const goToBest = () => { map.setView([best.lat, best.lon], Math.max(16, map.getZoom())); const p=target(); if(p) inspect(p); };
    $('best').addEventListener('click', goToBest);
    const tooltip = document.createElement('span'); tooltip.textContent = `Highest-scoring fit · ${format(best.score_db)} dB`;
    L.circleMarker([best.lat,best.lon], {radius: 10, color: '#fff', weight: 2, fillColor: '#17232d', fillOpacity: .25, pane: 'markerPane'}).bindTooltip(tooltip).on('click', goToBest).addTo(map);
  } else {
    $('best').textContent = 'No accepted location';
    $('best-score').textContent = `Every flyover must score at least ${format(report.min_event_score_db)} dB.`;
    $('best-clock').textContent = 'Below-threshold scores remain visible for inspection.';
  }

  const palette = ['#311445','#315de0','#10bfd4','#55d975','#f9e35a','#f58a35','#cf2634','#8e1028'].map(hex => [1,3,5].map(i=>parseInt(hex.slice(i,i+2),16)));
  function color(value, low, high) {
    const t = high > low ? Math.min(1, Math.max(0, (value-low)/(high-low))) : .5;
    const pos = t*(palette.length-1), a=Math.floor(pos), b=Math.min(a+1,palette.length-1), u=pos-a;
    return `rgb(${palette[a].map((v,i)=>Math.round(v*(1-u)+palette[b][i]*u)).join(',')})`;
  }
  let visible=[], low=0, high=0;
  const score = p => $('metric').value === 'combined' ? p.score_db : (p.event_scores_db || [])[Number($('metric').value)];
  function filter() {
    const stage=$('stage').value;
    visible=points.filter(p=>(stage==='all'||p.stage===stage)&&(!$('accepted-only').checked||p.meets_threshold)&&($('unscored').checked||finite(score(p))));
    // Keep the scale stable when filtering or panning; each metric uses the
    // entire search's score range, so colors remain comparable across views.
    low=Infinity;high=-Infinity;
    for(const p of points){const v=score(p);if(finite(v)){low=Math.min(low,v);high=Math.max(high,v);}}
    $('legend-min').textContent=finite(low)?`${format(low)} dB`:'No modeled scores';
    $('legend-max').textContent=finite(high)?`${format(high)} dB`:'';
    $('visible-count').textContent=`${visible.length.toLocaleString()} grid evaluations selected · gray = no complete fit`;
    dots.redraw();
  }
  // One canvas keeps large score grids practical without a DOM node per dot.
  const DotLayer=L.Layer.extend({
    onAdd() {
      const pane=map.getPane('scores')||map.createPane('scores');pane.style.zIndex=450;pane.style.pointerEvents='none';
      this.canvas=L.DomUtil.create('canvas','scores-canvas',pane);this.hits=[];
      map.on('moveend zoomend resize',this.redraw,this);map.on('click',this.click,this);this.redraw();
    },
    onRemove(){map.off('moveend zoomend resize',this.redraw,this);map.off('click',this.click,this);this.canvas.remove();},
    redraw(){
      if(!this.canvas)return;
      const size=map.getSize(),dpr=window.devicePixelRatio||1;
      L.DomUtil.setPosition(this.canvas,map.containerPointToLayerPoint([0,0]));
      this.canvas.width=size.x*dpr;this.canvas.height=size.y*dpr;this.canvas.style.width=`${size.x}px`;this.canvas.style.height=`${size.y}px`;
      const ctx=this.canvas.getContext('2d');ctx.scale(dpr,dpr);ctx.globalAlpha=Number($('opacity').value)/100;ctx.lineWidth=.65;
      const mpp=156543.03392*Math.cos(map.getCenter().lat*Math.PI/180)/2**map.getZoom();
      const view=map.getBounds().pad(.02);this.hits=[];
      for(const p of visible){
        if(!view.contains([p.lat,p.lon]))continue;
        const xy=map.latLngToContainerPoint([p.lat,p.lon]);const radius=Math.max(1.4,Math.min(6,p.grid_m/mpp*.22));const v=score(p);
        ctx.beginPath();ctx.arc(xy.x,xy.y,radius,0,Math.PI*2);
        ctx.fillStyle=finite(v)?color(v,low,high):'#a8b4b9';ctx.strokeStyle=finite(v)?'#19202c':'#e2e8eb';ctx.fill();ctx.stroke();
        this.hits.push({p,x:xy.x,y:xy.y,radius});
      }
    },
    click(e){
      let nearest=null,best=Infinity;
      for(const h of this.hits){const distance=Math.hypot(h.x-e.containerPoint.x,h.y-e.containerPoint.y);if(distance<=h.radius+4&&distance<best){nearest=h.p;best=distance;}}
      if(nearest)inspect(nearest);
    }
  });
  const dots=new DotLayer().addTo(map);
  ['metric','stage','accepted-only','unscored'].forEach(id=>$(id).addEventListener('change',filter));
  $('opacity').addEventListener('input',()=>{$('opacity-value').textContent=`${$('opacity').value}%`;dots.redraw();});
  $('fit').addEventListener('click',fit);
  filter();
})();
