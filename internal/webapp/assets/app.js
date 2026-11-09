(() => {
  'use strict';
  const $=id=>document.getElementById(id);
  let job=null, events=[], pollTimer=null, generation=0, openedResult=null, datasetReady=false, uploading=false;
  let weakFrom=0, nextWeak=0, weakEnd=false, weakLoading=false;
  const receivedEvents=info=>(info.events||[]).map((e,i)=>({...e,sourceIndex:info.event_indices?.[i]??i,enabled:!e.acoustic_evidence||e.acoustic_evidence.level==='strong'}));
  let config={max_upload_bytes:512*1024*1024};
  const busy=()=>uploading||(job&&['queued','analyzing','searching'].includes(job.state));
  const notify=message=>{$('notice').textContent=message;$('notice').hidden=!message;};
  async function api(path,options={}){const response=await fetch(path,options);let result;try{result=await response.json();}catch{throw new Error('The server returned an unreadable response. Check that just serve is running.');}if(!response.ok)throw new Error(result.error||'Request failed');return result;}
  const map=L.map('area-map',{zoomAnimation:false,maxZoom:19}).setView([38.5,-98],4);
  const context=PinpointMapContext(map,{onWarning:message=>{$('tile-warning').textContent=message;$('tile-warning').hidden=!message;}});
  function mapControls(){
    $('area-basemap').value=context.preferences.base;$('area-borders').checked=context.preferences.borders;$('area-roads').checked=context.preferences.roads;$('area-roads').disabled=context.preferences.base!=='satellite';$('plain-map').textContent=context.preferences.base==='none'?'Use satellite imagery':'Use plain background';
  }
  $('area-basemap').onchange=()=>{context.setBase($('area-basemap').value);mapControls();};
  $('area-borders').onchange=()=>context.setBorders($('area-borders').checked);
  $('area-roads').onchange=()=>context.setRoads($('area-roads').checked);
  mapControls();
  L.control.scale({imperial:false}).addTo(map);
  let selectedBounds=null;
  const areaDrawer=PinpointAreaSelection(map,{
    onSelect:b=>{selectedBounds=b;$('custom-area').checked=true;syncAreaFields(b);drawArea();},
    onClear:()=>{selectedBounds=null;$('custom-area').checked=false;drawArea();},
  });
  function validBounds(b){return b&&Object.values(b).every(Number.isFinite)&&b.south<b.north&&b.west<b.east&&b.south>=-84&&b.north<=84&&b.west>=-179&&b.east<=179;}
  function bounds(){
    if(!$('custom-area').checked)return undefined;
    if(selectedBounds)return {...selectedBounds};
    if(['lat','lon','width','area-height'].some(id=>!$(id).value))return null;
    const lat=Number($('lat').value),lon=Number($('lon').value),dy=Number($('area-height').value)*500/111320,dx=Number($('width').value)*500/(111320*Math.cos(lat*Math.PI/180));
    return {south:lat-dy,north:lat+dy,west:lon-dx,east:lon+dx};
  }
  function syncAreaFields(b){
    const lat=(b.north+b.south)/2;
    $('lat').value=lat.toFixed(6);$('lon').value=((b.east+b.west)/2).toFixed(6);
    $('width').value=((b.east-b.west)*111320*Math.cos(lat*Math.PI/180)/1000).toFixed(6);
    $('area-height').value=((b.north-b.south)*111320/1000).toFixed(6);
  }
  function drawArea(fit=false){
    context.highlightState($('state').value);
    const custom=$('custom-area').checked;
    for(const id of ['lat','lon','width','area-height'])$(id).disabled=!custom;
    const region=config.states?.find(s=>s.code===$('state').value);
    if(!custom){
      areaDrawer.setBounds(null);
      $('area-summary').textContent=region?`Searching within ${region.name}. Draw a rectangle to narrow the area.`:'No rectangle selected. Choose a state or draw a search area.';
      if(fit){if(region)map.fitBounds([[region.bounds.south,region.bounds.west],[region.bounds.north,region.bounds.east]],{padding:[30,30]});else map.setView([38.5,-98],4);}
      return;
    }
    const b=bounds();
    if(!validBounds(b)){areaDrawer.setBounds(null);$('area-summary').textContent='Enter a valid center, width and height, or draw a rectangle.';return;}
    areaDrawer.setBounds(b);
    const lat=(b.north+b.south)/2,width=(b.east-b.west)*111320*Math.cos(lat*Math.PI/180)/1000,height=(b.north-b.south)*111320/1000;
    $('area-summary').textContent=`Search rectangle: ${width.toFixed(2)} km wide × ${height.toFixed(2)} km high.${region?` Only the part within ${region.name} will be searched.`:''}`;
    if(fit)map.fitBounds([[b.south,b.west],[b.north,b.east]],{padding:[50,50],maxZoom:18});
  }
  $('map-center').onclick=()=>{
    areaDrawer.cancel();selectedBounds=null;$('custom-area').checked=true;
    if(Number($('width').value)>100){$('width').value=4;$('area-height').value=4;}
    const c=map.getCenter();$('lat').value=c.lat.toFixed(6);$('lon').value=c.lng.toFixed(6);drawArea(true);
  };
  ['lat','lon','width','area-height'].forEach(id=>$(id).addEventListener('change',()=>{areaDrawer.cancel();selectedBounds=null;drawArea(true);}));
  $('plain-map').onclick=()=>{context.setBase(context.preferences.base==='none'?'satellite':'none');mapControls();};
  const browserZone=Intl.DateTimeFormat().resolvedOptions().timeZone;if([...$('time-zone').options].some(o=>o.value===browserZone))$('time-zone').value=browserZone;
  drawArea();
  $('state').onchange=()=>{areaDrawer.cancel();selectedBounds=null;$('custom-area').checked=false;drawArea(true);};$('custom-area').onchange=()=>{areaDrawer.cancel();drawArea(true);};

  function sourceChanged(){const source=$('source').value;$('tracks-control').hidden=source!=='upload';$('data-hint').textContent=source==='archive'?'Flight history and terrain are downloaded as needed and cached. Same-day data may take time to appear.':source==='local'?'Use the flight dataset configured when starting this server.':'Use a normalized CSV containing aircraft positions, times and geometric heights.';}
  $('source').onchange=sourceChanged;
  function controls(){const active=busy();$('next-fragments').disabled=$('prev-fragments').disabled=active||weakLoading;$('detector-profile').disabled=active;$('find').disabled=!job||active||!job.last_time_s;$('sample').disabled=active;$('add-event').disabled=!job||active||!job.last_time_s;$('tracks').disabled=!job||active;$('cancel').hidden=!active;$('spinner').hidden=!active;$('reanalyze').hidden=$('reanalyze-hint').hidden=!job;$('reanalyze').disabled=!job||active;}
  function renderEvents(){
    $('events').replaceChildren();const strong=events.filter(e=>e.acoustic_evidence?.level==='strong').length,review=events.filter(e=>e.acoustic_evidence?.level==='review').length;$('event-count').textContent=events.some(e=>e.acoustic_evidence)?`${strong} strong curves · ${review} need review`:`${events.length} sound events`;$('event-filter-summary').textContent=$('show-weak').checked?'Showing up to 200 weak fragments per page alongside stronger curves. Use the page buttons to inspect more. Fragments are not confirmed aircraft.':'Showing strong curves, curves needing review, and manually added events. Ground traffic can also produce Doppler curves. Enable weak fragments to inspect all detector proposals.';
    let weakOrdinal=0;
    const weakCount=events.filter(e=>e.acoustic_evidence?.level==='weak').length;
    $('prev-fragments').hidden=!$('show-weak').checked||weakFrom===0;
    $('next-fragments').hidden=!$('show-weak').checked||(weakCount<=weakFrom+200&&(!job?.events_filtered||weakEnd));
    for(const [i,e] of events.entries()){
      if(e.acoustic_evidence?.level==='weak'){
        const slot=weakOrdinal++,selected=!$('automatic').checked&&e.enabled;
        if(!selected&&(!$('show-weak').checked||slot<weakFrom||slot>=weakFrom+200))continue;
      }
      const block=document.createElement('div');block.className='event';const label=document.createElement('label');const checkbox=document.createElement('input');checkbox.type='checkbox';checkbox.checked=e.enabled!==false;checkbox.onchange=()=>{e.enabled=checkbox.checked;};label.append(checkbox,document.createTextNode(`Sound event ${(e.sourceIndex??i)+1} · ${clockLabel(e.start_s)}–${clockLabel(e.end_s)}`));block.append(label);
      if(e.acoustic_evidence){const evidence=document.createElement('p');evidence.className='hint';evidence.textContent=`${e.acoustic_evidence.level==='strong'?'Strong acoustic candidate · aircraft unconfirmed':e.acoustic_evidence.level==='review'?'Needs review':'Weak fragment'}: ${e.acoustic_evidence.reasons.join(' ')}`;block.append(evidence);}
      if(e.measurement){const quality=document.createElement('p');quality.className='hint';quality.textContent=e.measurement.reason;block.append(quality);}
      for(const fields of [[['start_s','Start (s)'],['end_s','End (s)']],[['f0_min_hz','Source min (Hz)'],['f0_max_hz','Source max (Hz)']]]){const pair=document.createElement('div');pair.className='pair';for(const [key,title] of fields){const cell=document.createElement('div'),l=document.createElement('label'),input=document.createElement('input');input.type='number';input.step='any';input.value=e[key];input.setAttribute('aria-label',`Flyover ${i+1} ${title}`);input.oninput=()=>{e[key]=Number(input.value);};l.textContent=title;cell.append(l,input);pair.append(cell);}block.append(pair);}
      const filters=document.createElement('details');filters.className='event-filters';const filterTitle=document.createElement('summary');filterTitle.textContent='Measured CPA, speed and slant-distance bounds';filters.append(filterTitle);
      for(const [key,title]of [['cpa_s','Delayed CPA in recording (s)'],['cpa_tolerance_s','CPA uncertainty ± (s)'],['min_speed_mps','Minimum speed (m/s)'],['max_speed_mps','Maximum speed (m/s)'],['min_slant_m','Minimum slant distance (m)'],['max_slant_m','Maximum slant distance (m)']]){const label=document.createElement('label'),input=document.createElement('input');label.textContent=title;input.type='number';input.step='any';input.value=e[key]??0;input.setAttribute('aria-label',`Event ${i+1} ${title}`);input.oninput=()=>{e[key]=Number(input.value);};label.append(input);filters.append(label);}
      const partialLabel=document.createElement('label'),partial=document.createElement('input');partial.type='checkbox';partial.checked=!!e.partial_flyover;partial.onchange=()=>{e.partial_flyover=partial.checked;if(partial.checked){e.cpa_tolerance_s=0;renderEvents();}};partialLabel.append(partial,document.createTextNode(' Partial flyover: closest approach may fall outside this clip'));filters.append(partialLabel);block.append(filters);
      const inspect=document.createElement('button');inspect.type='button';inspect.className='text-button';inspect.textContent='View detected Doppler curve';inspect.disabled=!Number.isInteger(e.sourceIndex);inspect.onclick=()=>PinpointEvidence.open(`/api/jobs/${job.id}/detected/${e.sourceIndex+1}`);block.append(inspect);
      $('events').append(block);
    }controls();
  }
  function matchingControls(){
    const moving=$('moving').checked,mode=$('matching-mode');mode.querySelector('option[value="joint"]').disabled=moving;mode.querySelector('option[value="consensus"]').disabled=moving;
    if(moving&&(mode.value==='joint'||mode.value==='consensus'))mode.value='independent';
    $('automatic').disabled=false;$('include-weak').disabled=mode.value!=='independent';if(mode.value!=='independent')$('include-weak').checked=false;
    $('matching-hint').textContent=mode.value==='consensus'?'Finds locations where at least two strong curves are explained by different aircraft under one recording clock, then checks each area against the recording\'s loudness. Lists every near-tied area; nothing is verified.':mode.value==='joint'?'Automatic mode builds groups of two or more curves with one clock, fits training intervals, and checks frozen predictions on reserved audio and eight wrong-time searches. Reviewed mode requires every selected curve to fit together. Geometry uses at most 200 m, followed by 100 m scoring and finer searches around survivors.':'Each event gets its own candidate map and clock fit. This screens possible flights; it does not perform the video’s joint multi-flight verification.';
  }
  async function loadWeakPage(){
    if(!job||busy()||weakLoading||weakEnd)return;
    const id=job.id,token=generation;
    weakLoading=true;controls();
    try{
      const page=await api(`/api/jobs/${id}/fragments?from=${nextWeak}&limit=200`);
      if(job?.id!==id||generation!==token)return;
      weakFrom=events.filter(e=>e.acoustic_evidence?.level==='weak').length;
      const existing=new Set(events.map(e=>e.sourceIndex));
      for(let i=0;i<page.events.length;i++)if(!existing.has(page.event_indices[i]))events.push({...page.events[i],sourceIndex:page.event_indices[i],enabled:false});
      events.sort((a,b)=>a.start_s-b.start_s);nextWeak=page.next;weakEnd=page.end;renderEvents();
    }catch(e){if(job?.id===id&&generation===token)notify(e.message);}
    finally{if(generation===token){weakLoading=false;controls();}}
  }
  $('show-weak').onchange=()=>{renderEvents();if($('show-weak').checked&&job?.events_filtered&&!events.some(e=>e.acoustic_evidence?.level==='weak'))loadWeakPage();};
  $('next-fragments').onclick=()=>{const n=events.filter(e=>e.acoustic_evidence?.level==='weak').length;if(weakFrom+200<n){weakFrom+=200;renderEvents();}else loadWeakPage();};
  $('prev-fragments').onclick=()=>{weakFrom=Math.max(0,weakFrom-200);renderEvents();};
  $('moving').addEventListener('change',()=>{matchingControls();if(job)renderCoverage(job);});$('matching-mode').onchange=()=>{if($('matching-mode').value==='joint'){$('grid').value=Math.min(200,Number($('grid').value));$('refine').value=Math.min(100,Number($('refine').value));}matchingControls();if(job)renderCoverage(job);};
  let suggestedSilence=[];
  $('suggest-silence').onclick=()=>{
    suggestedSilence=[];let end=job?.first_time_s||0;
    for(const e of [...events].sort((a,b)=>a.start_s-b.start_s)){
      if(e.start_s-10-end>=20)suggestedSilence.push({start_s:Math.ceil(end),end_s:Math.floor(e.start_s-10),max_slant_m:2000});
      end=Math.max(end,e.end_s+10);
    }
    if((job?.last_time_s||0)-end>=20)suggestedSilence.push({start_s:Math.ceil(end),end_s:Math.floor(job.last_time_s),max_slant_m:2000});
    $('silence-suggestions').hidden=false;$('silence-suggestions').textContent=suggestedSilence.length?`Gaps between displayed curves — listen before using: ${suggestedSilence.map(w=>`${clockLabel(w.start_s)}–${clockLabel(w.end_s)}`).join(', ')}. Wind, speech and microphone processing can hide aircraft.`:'No gaps of at least 20 seconds were found.';
    $('silence-review-label').hidden=$('apply-silence').hidden=!suggestedSilence.length;$('silence-review').checked=false;$('apply-silence').disabled=true;
  };
  $('silence-review').onchange=()=>{$('apply-silence').disabled=!$('silence-review').checked;};
  $('apply-silence').onclick=()=>{$('silence').value=suggestedSilence.map(w=>`${w.start_s}, ${w.end_s}, ${w.max_slant_m}`).join('\n');$('silence').closest('details').open=true;};
  $('select-none').onclick=()=>{for(const e of events)e.enabled=false;$('automatic').checked=false;renderEvents();};
  matchingControls();
  $('add-event').onclick=()=>{events.push({start_s:job.first_time_s,end_s:job.last_time_s,cpa_s:(job.first_time_s+job.last_time_s)/2,f0_min_hz:80,f0_max_hz:200,f0_step_hz:1,harmonics:[1],min_speed_mps:20,max_speed_mps:160,min_slant_m:100,max_slant_m:10000,enabled:true});renderEvents();};
  function restoreSelection(p){
    if(p.automatic||!p.events)return;
    const remaining=new Set(p.events);
    events=events.map(e=>{const selected=p.events.find(q=>q.ridge?.length&&e.ridge?.length?e.ridge.some(point=>point.time_s===q.ridge[0].time_s&&point.hz===q.ridge[0].hz):q.start_s===e.start_s&&q.end_s===e.end_s);if(selected)remaining.delete(selected);return {...(selected||e),sourceIndex:e.sourceIndex,enabled:!!selected};});
    for(const e of remaining)events.push({...e,enabled:true});
    renderEvents();
  }
  function applyParams(p){$('exclude-jets').checked=p.exclude_known_jets===true;$('include-weak').checked=p.include_weak_candidates===true;$('silence').value=(p.silence_intervals||[]).map(w=>`${w.start_s}, ${w.end_s}, ${w.max_slant_m}`).join('\n');$('state').value=p.state||'';$('moving').checked=!!p.moving;$('automatic').checked=!!p.automatic;$('matching-mode').value=p.match_mode||((p.automatic||p.moving)?'independent':'joint');matchingControls();$('time-zone').value=p.time_zone||'UTC';$('recorded-at').value=p.local_start||new Date(p.recording_start).toISOString().slice(0,19);const region=config.states?.find(s=>s.code===p.state)?.bounds||{south:24.39,north:49.39,west:-124.85,east:-66.88};$('custom-area').checked=!!p.bounds&&Object.keys(region).some(k=>Math.abs(p.bounds[k]-region[k])>1e-6);$('height').value=p.observer_height_ellipsoid_m??'';if(![...$('uncertainty').options].some(o=>o.value===String(p.clock_uncertainty_s))){const o=document.createElement('option');o.value=p.clock_uncertainty_s;o.textContent=`Within ${p.clock_uncertainty_s} seconds`;$('uncertainty').append(o);}$('uncertainty').value=p.clock_uncertainty_s;$('grid').value=p.grid_m;$('refine').value=p.refine_m;$('final-grid').value=p.final_grid_m||10;areaDrawer.cancel();selectedBounds=p.bounds?{...p.bounds}:null;if(selectedBounds)syncAreaFields(selectedBounds);$('source').value=[...$('source').options].some(o=>o.value===p.source)?p.source:'archive';sourceChanged();drawArea(true);restoreSelection(p);}
  function showResults(){const url=$('event-map').value||job?.result_url;if(!url||url.startsWith('pending-'))return;$('results').src=url+'?embedded=1';$('results').hidden=false;$('workspace').hidden=true;$('result-nav').hidden=false;$('local-badge').hidden=true;$('open-map').href=url;openedResult=job.result_url||url;}
  $('event-map').onchange=showResults;
  $('adjust').onclick=()=>{$('results').hidden=true;$('workspace').hidden=false;$('result-nav').hidden=true;$('local-badge').hidden=false;setTimeout(()=>map.invalidateSize(),0);};
  const clockLabel=t=>`${Math.floor(t/60)}:${String(Math.floor(t%60)).padStart(2,'0')}`;
  const outcomeName={validation_pass:'Reserved checks pass · unverified',validation_failed:'Reserved audio rejected',pending:'Waiting',searching:'Searching',candidate_matches:'Candidate matches · unverified',no_match:'No supporting match',no_tracks:'No trajectory data in this interval',error:'Failed',not_run:'Not searched'};
  function renderCoverage(info){
    const d=info.detection;
    $('coverage-card').hidden=!d;
    if(d){
      $('coverage-summary').textContent=`Scanned ${clockLabel(d.scanned_start_s)}–${clockLabel(d.scanned_end_s)}. ${info.total_fragments??d.events?.length??info.events?.length??0} tonal fragments; ${d.strong_curve_count===undefined?'Evidence checks need refreshing; click Reanalyze recording.':`${d.strong_curve_count} strong acoustic curves, ${d.review_curve_count??(info.events||[]).filter(e=>e.acoustic_evidence?.level==='review').length} needing review, ${d.joint_curve_count} with bounded closest approach. Strong partial curves can also constrain joint matching.`} These are not aircraft counts or calibrated confidence levels. Weak fragments are excluded from automatic search unless diagnostic mode is enabled.`;
      $('detection-report').href=`/api/jobs/${info.id}/files/detection.json`;
      $('coverage-timeline').replaceChildren();
      for(const w of d.windows||[]){const block=document.createElement('span');block.className=w.candidates?'has-candidates':'';block.title=`${clockLabel(w.start_s)}–${clockLabel(w.end_s)}: ${w.candidates} tonal fragments`;block.setAttribute('aria-label',block.title);$('coverage-timeline').append(block);}
    }
    const outcomes=info.event_maps||[];
    $('event-results').hidden=!outcomes.length;
    const complete=outcomes.filter(m=>['candidate_matches','validation_pass','validation_failed','no_match','no_tracks'].includes(m.status)).length;
    const failed=outcomes.filter(m=>['error','not_run'].includes(m.status)).length;
    $('event-results-summary').textContent=(info.params?.automatic&&info.params?.match_mode==='joint'&&!info.synthetic)?`${outcomes.length} sound-group maps · selection and controls in the overview`:`${complete}/${outcomes.length} events searched${failed?` · ${failed} unfinished`:''}`;
    $('event-outcomes').replaceChildren();
    const selected=$('event-map').value;
    $('event-map').replaceChildren();
    const groupOverview=info.result_url?.endsWith('/automatic.html');
    if(groupOverview){const option=document.createElement('option');option.value=info.result_url;option.textContent='All groups and timing controls';$('event-map').append(option);}
    for(const m of outcomes){
      const row=document.createElement('div');row.className='event-outcome';
      const label=document.createElement(m.url?'button':'strong');label.textContent=m.label;
      if(m.url){label.type='button';label.className='text-button';label.onclick=()=>{$('event-map').value=m.url;showResults();};}
      const status=document.createElement('p');status.textContent=outcomeName[m.status]||m.status;row.append(label,status);
      if(m.status==='searching'&&m.message){const progress=document.createElement('p');progress.className='event-progress';progress.textContent=m.message;row.append(progress);}
      if(m.error){const error=document.createElement('p');error.className='event-error';error.textContent=m.error;row.append(error);}
      if(m.url){const evidence=document.createElement('button');evidence.type='button';evidence.className='text-button';evidence.textContent='Compare Doppler curves';evidence.onclick=()=>PinpointEvidence.open(m.url.replace(/map\.html$/,'evidence.json'));row.append(evidence);}
      $('event-outcomes').append(row);
      const option=document.createElement('option');option.value=m.url||`pending-${outcomes.indexOf(m)}`;option.disabled=!m.url;option.textContent=`${m.label} · ${outcomeName[m.status]||m.status}`;$('event-map').append(option);
    }
    const target=(openedResult&&(!info.result_url||openedResult===info.result_url)?outcomes.find(m=>m.url===selected&&m.url)?.url:null)||info.result_url;
    if(target)$('event-map').value=target;
    $('event-map').hidden=!outcomes.length&&!groupOverview;
    $('view-results').hidden=!info.result_url;
  }
  $('view-results').onclick=showResults;
  function render(info,initial=false){
    const previous=job;job=info;datasetReady=info.has_tracks;if(initial||previous?.state==='analyzing'&&info.state==='ready')$('detector-profile').value=info.detector_profile||'standard';
    $('file-label').textContent=info.filename+(info.synthetic?' · sample':'');
    $('progress-card').hidden=false;$('phase-title').textContent=({queued:'Queued',analyzing:'Analyzing recording',ready:'Audio analysis ready',waiting_data:'Waiting for flight history',searching:'Finding likely locations',complete:info.result_status==='partial'||info.result_status==='incomplete'?'Search partially completed':info.result_status==='unresolved'?'Location unresolved':info.result_status==='controls_not_run'?'Controls not run':info.result_status==='no_match'?'No supporting location match':'Candidate maps ready',error:'Run could not finish',cancelled:'Run cancelled'})[info.state]||info.state;
    $('progress-message').textContent=info.message;$('progress').hidden=!(info.total>0&&busy());if(info.total>0){$('progress').max=info.total;$('progress').value=info.completed;}
    $('spectrogram').hidden=!info.last_time_s;$('spectrogram').href=`/api/jobs/${info.id}/files/spectrogram.svg`;
    if(initial||previous?.state==='analyzing'&&info.state==='ready'||!previous?.last_time_s&&info.last_time_s||previous?.detection?.version!==info.detection?.version||previous?.events?.length!==info.events?.length){events=receivedEvents(info);weakFrom=nextWeak=0;weakEnd=!info.events_filtered;weakLoading=false;renderEvents();}
    if(initial&&!info.params){const m=info.filename.match(/(20\d{2})[_-](\d{2})[_-](\d{2})[_T -](\d{2})[_:-](\d{2})[_:-](\d{2})/);if(m){$('recorded-at').value=`${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}`;$('filename-time').textContent='Suggested by filename; check the time and time zone.';}}
    if(initial&&info.params||info.synthetic&&!previous?.params&&info.params){applyParams(info.params);}
    $('tracks-status').textContent=datasetReady?'Flight tracks are loaded for this recording.':'Upload normalized flight tracks for this recording.';
    if(info.state==='error')notify(info.error||info.message);
    controls();
    renderCoverage(info);
    if(info.state==='complete'&&info.result_url&&openedResult!==info.result_url)showResults();
  }
  async function poll(id,token){try{const info=await api(`/api/jobs/${id}`);if(token!==generation)return;render(info);if(busy())pollTimer=setTimeout(()=>poll(id,token),800);}catch(e){if(token===generation)notify(e.message+' Refresh to reconnect.');}}
  function activate(info){suggestedSilence=[];$('silence-suggestions').hidden=true;$('silence-review-label').hidden=true;$('apply-silence').hidden=true;$('silence-review').checked=false;$('apply-silence').disabled=true;if(!info.params){$('exclude-jets').checked=false;$('include-weak').checked=false;$('show-weak').checked=false;$('silence').value='';$('automatic').checked=true;$('matching-mode').value=$('moving').checked?'independent':'joint';matchingControls();}clearTimeout(pollTimer);generation++;job=null;openedResult=null;events=[];$('results').hidden=true;$('workspace').hidden=false;$('result-nav').hidden=true;try{localStorage.setItem('pinpoint-job',info.id);}catch{}history.replaceState(null,'',`/?job=${info.id}`);$('resume-recording').hidden=true;render(info,true);if(busy())pollTimer=setTimeout(()=>poll(info.id,generation),500);}
  async function upload(file){
    if(!file)return;if(busy()){notify('Cancel the current run before uploading another recording.');return;}if(!file.size){notify('This file is empty. Choose an audio recording.');return;}if(file.size>config.max_upload_bytes){notify(`The upload limit is ${Math.floor(config.max_upload_bytes/1024/1024)} MiB.`);return;}
    notify('');$('filename-time').textContent='';const timestamp=file.name.match(/(20\d{2})[_-](\d{2})[_-](\d{2})[_T -](\d{2})[_:-](\d{2})[_:-](\d{2})/);if(timestamp){$('recorded-at').value=`${timestamp[1]}-${timestamp[2]}-${timestamp[3]}T${timestamp[4]}:${timestamp[5]}:${timestamp[6]}`;$('filename-time').textContent='Start time suggested by the filename. Check it and select the recording time zone.';}else{$('recorded-at').value='';}if(job){if(job.synthetic){$('height').value='';$('source').value=config.local_tracks_available?'local':'archive';sourceChanged();}}uploading=true;controls();$('file-label').textContent=`Uploading ${file.name}…`;const form=new FormData();form.append('audio',file);form.append('detector_profile',$('detector-profile').value);
    try{const info=await api('/api/jobs',{method:'POST',body:form});uploading=false;activate(info);}catch(e){uploading=false;notify(e.message);controls();}
  }
  $('dropzone').onclick=()=>$('audio').click();$('dropzone').onkeydown=e=>{if(e.key==='Enter'||e.key===' '){e.preventDefault();$('audio').click();}};$('audio').onchange=()=>upload($('audio').files[0]);
  for(const type of ['dragenter','dragover'])$('dropzone').addEventListener(type,e=>{e.preventDefault();$('dropzone').classList.add('drag');});
  for(const type of ['dragleave','drop'])$('dropzone').addEventListener(type,e=>{e.preventDefault();$('dropzone').classList.remove('drag');});
  $('dropzone').addEventListener('drop',e=>upload(e.dataTransfer.files[0]));window.addEventListener('dragover',e=>e.preventDefault());window.addEventListener('drop',e=>e.preventDefault());
  $('sample').onclick=async()=>{notify('');$('sample').disabled=true;try{activate(await api('/api/demo',{method:'POST'}));}catch(e){notify(e.message);controls();}};
  $('reanalyze').onclick=async()=>{
    if(!job||busy())return;
    notify('');$('reanalyze').disabled=true;openedResult=null;
    try{const info=await api(`/api/jobs/${job.id}/reanalyze`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({detector_profile:$('detector-profile').value})});render(info);clearTimeout(pollTimer);generation++;pollTimer=setTimeout(()=>poll(info.id,generation),500);}catch(e){notify(e.message);controls();}
  };
  $('cancel').onclick=async()=>{if(!job)return;try{await api(`/api/jobs/${job.id}/cancel`,{method:'POST'});$('progress-message').textContent='Cancelling…';}catch(e){notify(e.message);}};
  $('tracks').onchange=async()=>{const file=$('tracks').files[0];if(!job||!file)return;notify('');$('find').disabled=true;$('tracks-status').textContent='Reading flight tracks…';const form=new FormData();form.append('tracks',file);try{const result=await api(`/api/jobs/${job.id}/tracks`,{method:'POST',body:form});datasetReady=true;job.has_tracks=true;$('tracks-status').textContent=`${result.tracks} aircraft tracks loaded.`;}catch(e){notify(e.message);}controls();};
  $('search-form').onsubmit=async e=>{
    e.preventDefault();notify('');if(!job||busy())return;if(!$('search-form').reportValidity())return;
    if($('source').value==='upload'&&!datasetReady){notify('Choose a flight-track CSV, or select automatic flight-history download.');return;}
    const selected=events.filter(e=>e.enabled!==false).map(e=>{const {enabled,sourceIndex,...event}=e;if(!Number.isFinite(event.cpa_s))event.cpa_s=(event.start_s+event.end_s)/2;if(event.ridge)event.ridge=event.ridge.filter(p=>p.time_s>=event.start_s&&p.time_s<=event.end_s);return event;}).sort((a,b)=>a.start_s-b.start_s);
    if(!selected.length&&!$('automatic').checked){notify('No flyovers are selected. Review the spectrogram and add an event, or try another recording.');$('events-panel').open=true;return;}
    if($('matching-mode').value==='joint'&&!$('automatic').checked&&selected.length<2){notify('Review and select at least two distinct flyovers to match jointly.');return;}
    if(areaDrawer.isDrawing()){notify('Finish drawing the rectangle or cancel drawing before starting the search.');return;}
    if($('custom-area').checked&&!validBounds(bounds())){notify('Draw a valid rectangle or enter a valid center, width and height.');return;}
    const silence=$('silence').value.trim()?$('silence').value.trim().split(/\n/).map(line=>{const [start_s,end_s,max_slant_m]=line.split(',').map(Number);return {start_s,end_s,max_slant_m};}):[];
    if(silence.some(w=>!Object.values(w).every(Number.isFinite))){notify('Each silence interval needs three numbers: start seconds, end seconds, radius metres.');return;}
    const request={include_weak_candidates:$('include-weak').checked,exclude_known_jets:$('exclude-jets').checked,silence_intervals:silence,match_mode:$('matching-mode').value,local_start:$('recorded-at').value,time_zone:$('time-zone').value,state:$('state').value,moving:$('moving').checked,automatic:$('automatic').checked,bounds:bounds(),observer_height_ellipsoid_m:$('height').value===''?null:Number($('height').value),clock_uncertainty_s:Number($('uncertainty').value),source:$('source').value,grid_m:Math.min($('matching-mode').value!=='independent'?200:Infinity,Number($('grid').value)),refine_m:Number($('refine').value),final_grid_m:Number($('refine').value)>0?Math.min(Number($('refine').value),Number($('final-grid').value)):0};
    if(!request.automatic)request.events=selected;

    $('find').disabled=true;openedResult=null;
    try{const info=await api(`/api/jobs/${job.id}/search`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(request)});render(info);clearTimeout(pollTimer);generation++;pollTimer=setTimeout(()=>poll(info.id,generation),500);}catch(error){notify(error.message);try{const info=await api(`/api/jobs/${job.id}`);job=info;events=receivedEvents(info);weakFrom=nextWeak=0;weakEnd=!info.events_filtered;weakLoading=false;renderEvents();renderCoverage(info);}catch{}controls();}
  };
  // Only an explicit job URL resumes a job. Remembering the last recording
  // supplies a link; it must not redirect a fresh visit away from the uploader.
  (async()=>{
    try{
      config=await api('/api/config');
      for(const state of config.states||[]){const option=document.createElement('option');option.value=state.code;option.textContent=state.name;$('state').append(option);}
      if(config.local_tracks_available){const option=document.createElement('option');option.value='local';option.textContent='Use configured local flight dataset';$('source').append(option);$('source').value='local';sourceChanged();}
      const id=new URLSearchParams(location.search).get('job');
      if(id&&/^[a-f0-9]{32}$/.test(id)){
        activate(await api(`/api/jobs/${id}`));
      }else{
        try{
          const previous=localStorage.getItem('pinpoint-job');
          if(previous&&/^[a-f0-9]{32}$/.test(previous)){$('resume-recording').href=`/?job=${previous}`;$('resume-recording').hidden=false;}
        }catch{}
        controls();
      }
    }catch(e){notify(e.message);}
  })();
})();
