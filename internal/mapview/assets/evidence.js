(() => {
  'use strict';
  const node=(tag,cls,text)=>{const n=document.createElement(tag);if(cls)n.className=cls;if(text!==undefined)n.textContent=text;return n;};
  const svgNode=(tag,attrs,text)=>{const n=document.createElementNS('http://www.w3.org/2000/svg',tag);for(const [key,value]of Object.entries(attrs||{}))n.setAttribute(key,value);if(text!==undefined)n.textContent=text;return n;};
  let dialog,controller,serial=0;
  function create(){
    dialog=node('dialog','doppler-dialog');dialog.setAttribute('aria-label','Doppler curve evidence');document.body.append(dialog);
    dialog.addEventListener('close',()=>controller?.abort());
    dialog.addEventListener('click',e=>{if(e.target===dialog){const r=dialog.getBoundingClientRect();if(e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom)dialog.close();}});
  }
  function header(){
    const bar=node('div','doppler-header');bar.append(node('h2','','Doppler curve evidence'));
    const close=node('button','doppler-close','Close');close.type='button';close.onclick=()=>dialog.close();bar.append(close);dialog.append(bar);
  }
  function plot(event,model,index,options,total){
    const section=node('section','doppler-event');
    section.append(node('h3','',`${total>1?`Sound event ${index+1}`:'Sound interval'} · ${event.event.start_s.toFixed(1)}–${event.event.end_s.toFixed(1)} s`));
    section.append(node('p','doppler-match',model?`${model.track_id} · source tone ${model.f0_hz.toFixed(2)} Hz · event score ${model.score_db.toFixed(2)} dB`:'Recorded sound only · no accepted flight prediction available'));
    if(model?.reserved_validation){const v=model.reserved_validation;section.append(node('p','doppler-match',`Reserved audio: ${v.pass?'passes checks':'rejected'} · source curve ${v.event_index+1} · ${v.training_samples} training / ${v.validation_samples} reserved samples${Number.isFinite(v.validation_ridge_rmse_hz)?` · reserved frequency error ${v.validation_ridge_rmse_hz.toFixed(2)} Hz`:""}${Number.isFinite(v.validation_margin_db)?` · ${v.validation_margin_db.toFixed(2)} dB over tone controls`:''}. Location, clock, aircraft and source frequency stay fixed.`));for(const reason of v.rejections||[])section.append(node('p','doppler-muted',`${reason.code}: ${reason.detail}`));}
    if(model?.fit_support){const q=model.fit_support;const details=[];if(Number.isFinite(q.ridge_rmse_hz))details.push(`Frequency error ${q.ridge_rmse_hz.toFixed(2)} Hz RMS across ${q.ridge_samples} ridge samples`);if(Number.isFinite(q.median_model_sideband_contrast_db))details.push(`Median tone contrast ${q.median_model_sideband_contrast_db.toFixed(2)} dB`);if(details.length)section.append(node('p','doppler-muted',`${details.join(' · ')}. Checked at every spectrogram frame with this location and clock fixed; these are fit diagnostics, not independent validation.`));}
    if(event.event.cpa_tolerance_s>0)section.append(node('p','doppler-muted',`Measured delayed CPA: ${event.event.cpa_s.toFixed(2)} ± ${event.event.cpa_tolerance_s.toFixed(2)} s${Number.isFinite(model?.delayed_cpa_s)?` · predicted: ${model.delayed_cpa_s.toFixed(2)} s`:''}`));
    if(index>0&&Number.isFinite(event.start_offset_from_first_s))section.append(node('p','doppler-muted',`This interval starts ${event.start_offset_from_first_s.toFixed(2)} s after event 1. Every measured sample keeps that timing under the shared clock.${event.event.partial_flyover?' Partial curve: no closest-approach time is assumed.':''}`));
    if(model?.relative_timing){const t=model.relative_timing;section.append(node('p','doppler-muted',`Arrival gap from event ${t.reference_event}: recorded ${t.observed_gap_s.toFixed(2)} ± ${t.tolerance_s.toFixed(2)} s · predicted ${t.predicted_gap_s.toFixed(2)} s · difference ${t.residual_s.toFixed(2)} s. Changing the common clock cannot change this gap.`));}
    if(event.event.measurement){const m=event.event.measurement;section.append(node('p','doppler-muted',m.reason+(m.f0_hz>0?` Ideal Doppler RMS ${m.doppler_rmse_hz.toFixed(2)} Hz; linear trend RMS ${m.linear_rmse_hz.toFixed(2)} Hz.`:'')));}
    if(event.event.acoustic_evidence){const q=event.event.acoustic_evidence;section.append(node('p','doppler-muted',`${q.level==='strong'?'Strong acoustic evidence · aircraft unconfirmed':q.level==='review'?'Needs review':'Weak tonal fragment'}. Coverage ${(q.ridge_coverage*100).toFixed(0)}%; contrast ${q.median_contrast_db.toFixed(1)} dB; ${q.resolution_count} resolution(s); harmonic support ${(q.harmonic_support*100).toFixed(0)}%. ${q.heldout_doppler_rmse_hz!==undefined?`Held-out RMS ${q.heldout_doppler_rmse_hz.toFixed(2)} Hz versus linear ${q.heldout_linear_rmse_hz.toFixed(2)} Hz. `:''}${q.reasons.join(' ')}`));}
    const image=event.spectrum;
    const W=dialog.clientWidth<600?600:900,H=330,left=65,right=20,top=20,bottom=48,pw=W-left-right,ph=H-top-bottom;
    const x=t=>left+(t-image.start_s)/(image.end_s-image.start_s)*pw;
    const y=hz=>top+(image.max_hz-hz)/(image.max_hz-image.min_hz)*ph;
    const svg=svgNode('svg',{viewBox:`0 0 ${W} ${H}`,role:'img','aria-label':`Spectrogram and Doppler curves for sound event ${index+1}`});
    const clip=`doppler-clip-${++serial}`;const defs=svgNode('defs');const clipPath=svgNode('clipPath',{id:clip});clipPath.append(svgNode('rect',{x:left,y:top,width:pw,height:ph}));defs.append(clipPath);svg.append(defs);
    svg.append(svgNode('rect',{x:left,y:top,width:pw,height:ph,fill:'#060b10'}));
    if(image.data_url.startsWith('data:image/png;base64,'))svg.append(svgNode('image',{x:left,y:top,width:pw,height:ph,href:image.data_url,preserveAspectRatio:'none',style:`filter:brightness(${options.contrast})`}));
    for(let k=0;k<=5;k++){
      const xx=left+pw*k/5,yy=top+ph*k/5;
      svg.append(svgNode('line',{x1:xx,y1:top,x2:xx,y2:top+ph,stroke:'#a0b6c1','stroke-opacity':'.15'}));
      svg.append(svgNode('line',{x1:left,y1:yy,x2:left+pw,y2:yy,stroke:'#a0b6c1','stroke-opacity':'.15'}));
      svg.append(svgNode('text',{x:xx,y:H-23,'text-anchor':'middle',fill:'#b8c9d2'},(image.start_s+(image.end_s-image.start_s)*k/5).toFixed(1)));
      svg.append(svgNode('text',{x:left-9,y:yy+5,'text-anchor':'end',fill:'#b8c9d2'},(image.max_hz-(image.max_hz-image.min_hz)*k/5).toFixed(1)));
    }
    svg.append(svgNode('text',{x:W/2,y:H-3,'text-anchor':'middle',fill:'#b8c9d2'},'Time in recording (seconds)'));
    svg.append(svgNode('text',{x:16,y:H/2,transform:`rotate(-90 16 ${H/2})`,'text-anchor':'middle',fill:'#b8c9d2'},'Frequency (Hz)'));
    for(const t of [event.event.start_s,event.event.end_s])svg.append(svgNode('line',{x1:x(t),y1:top,x2:x(t),y2:top+ph,stroke:'#9ce1cf','stroke-dasharray':'5 5','stroke-opacity':'.55'}));
    const curves=svgNode('g',{'clip-path':`url(#${clip})`});
    if(model?.reserved_validation?.partition){const p=model.reserved_validation.partition;for(let at=p.Start;at<p.End;at+=2*p.Block){for(const [from,to,color] of [[at+p.Guard,Math.min(p.End,at+p.Block-p.Guard),'#90d8bc'],[at+p.Block+p.Guard,Math.min(p.End,at+2*p.Block-p.Guard),'#c29aed']])if(to>from)curves.append(svgNode('rect',{x:x(from),y:top,width:x(to)-x(from),height:ph,fill:color,'fill-opacity':'.12'}));}}
    if(event.event.cpa_tolerance_s>0){const e=event.event;curves.append(svgNode('rect',{x:x(e.cpa_s-e.cpa_tolerance_s),y:top,width:x(e.cpa_s+e.cpa_tolerance_s)-x(e.cpa_s-e.cpa_tolerance_s),height:ph,fill:'#dc9be8','fill-opacity':'.09'}));curves.append(svgNode('line',{x1:x(e.cpa_s),x2:x(e.cpa_s),y1:top,y2:top+ph,stroke:'#dc9be8','stroke-dasharray':'3 4'}));}
    const line=(points,color,gap)=>{
      let path='',last=-Infinity;
      for(const p of points){if(!Number.isFinite(p.time_s)||!Number.isFinite(p.hz))continue;path+=`${!path||p.time_s-last>gap?'M':'L'}${x(p.time_s).toFixed(2)},${y(p.hz).toFixed(2)} `;last=p.time_s;}
      curves.append(svgNode('path',{d:path,fill:'none',stroke:color,'stroke-width':2.2,'vector-effect':'non-scaling-stroke','data-curve':color==='#6ce9fc'?'observed':'predicted'}));
    };
    if(options.observed&&event.event.ridge?.length)line(event.event.ridge,'#6ce9fc',4);
    if(options.predicted&&model)for(const segment of model.segments)line(segment,'#ffac55',Infinity);
    svg.append(curves);
    const scroll=node('div','doppler-plot');scroll.append(svg);section.append(scroll);
    if(!event.event.ridge?.length)section.append(node('p','doppler-muted','This reviewed event has no traced ridge; compare the prediction directly with the recorded spectrogram.'));
    return section;
  }
  function render(data){
    dialog.replaceChildren();header();
    const body=node('div','doppler-body');dialog.append(body);
    body.append(node('p','doppler-source',data.source),node('p','doppler-method',data.method));
    const candidates=data.candidates||[];
    const controls=node('div','doppler-controls');body.append(controls);
    const select=node('select');select.setAttribute('aria-label','Candidate flight match');
    if(candidates.length){for(const [i,c]of candidates.entries()){const option=node('option','',`${i+1}. ${c.events.map(e=>e.track_id).join(' + ')} · ${c.score_db.toFixed(2)} dB`);option.value=i;select.append(option);}controls.append(select);}
    const settings={observed:true,predicted:true,contrast:1};
    function checkbox(key,title){const label=node('label');const box=node('input');box.type='checkbox';box.checked=settings[key];if(key==='predicted'&&!candidates.length){box.disabled=true;box.checked=false;settings.predicted=false;}box.onchange=()=>{settings[key]=box.checked;draw();};label.append(box,document.createTextNode(title));controls.append(label);}
    checkbox('observed','Detected ridge');checkbox('predicted','Flight prediction');
    const contrastLabel=node('label','','Spectrogram brightness');const contrast=node('input');contrast.type='range';contrast.min=.5;contrast.max=2.5;contrast.step=.1;contrast.value=1;contrast.setAttribute('aria-label','Spectrogram brightness');contrast.oninput=()=>{settings.contrast=Number(contrast.value);draw();};contrastLabel.append(contrast);controls.append(contrastLabel);
    body.append(node('p','doppler-legend','White: measured spectrum · Cyan: detected ridge · Orange: flight prediction · Dashed: scored interval · Where validation is available: green = training, purple = reserved; gaps are guards'));
    const summary=node('p','doppler-candidate');body.append(summary);
    const plots=node('div');body.append(plots);body.append(node('p','doppler-muted',data.note||''));
    function draw(){
      const rank=Number(select.value)||0,c=candidates[rank];
      summary.textContent=c?`${c.lat.toFixed(6)}, ${c.lon.toFixed(6)} · recording clock ${c.clock_offset_s>=0?'+':''}${c.clock_offset_s.toFixed(2)} s${data.events.length>1?' (shared by every event)':''}`:'No candidate match is being asserted.';
      plots.replaceChildren(...data.events.map((event,i)=>plot(event,event.models?.[rank],i,settings,data.events.length)));
    }
    select.onchange=draw;draw();
  }
  window.PinpointEvidence={async open(source){
    if(!dialog)create();controller?.abort();controller=new AbortController();const request=controller;
    dialog.replaceChildren();header();dialog.append(node('p','doppler-loading','Loading recorded sound and flight curves…'));if(!dialog.open)dialog.showModal();
    try{
      let data=source;
      if(typeof source==='string'){const response=await fetch(source,{signal:request.signal});data=await response.json();if(!response.ok)throw new Error(data.error||'Evidence is unavailable.');}
      if(request!==controller||!dialog.open)return;render(data);
    }catch(error){if(error.name==='AbortError')return;dialog.replaceChildren();header();dialog.append(node('p','doppler-loading',error.message));}
  }};
})();
