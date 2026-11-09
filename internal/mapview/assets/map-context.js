// Shared geographic context for the upload map and portable result maps.
window.PinpointMapContext = function(map, options = {}) {
  const defaults={base:'satellite',roads:true,borders:true};
  let preferences={...defaults};
  try { Object.assign(preferences,JSON.parse(localStorage.getItem('pinpoint-map-layers')||'{}')); } catch {}
  if(!['satellite','streets','none'].includes(preferences.base))preferences.base='satellite';
  const save=()=>{try{localStorage.setItem('pinpoint-map-layers',JSON.stringify(preferences));}catch{}};
  const esri='https://server.arcgisonline.com/ArcGIS/rest/services/';
  const credits='Esri, HERE, Garmin, © <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap</a> contributors';
  const tile=(path,attribution,pane)=>L.tileLayer(esri+path+'/MapServer/tile/{z}/{y}/{x}',{maxNativeZoom:19,maxZoom:21,keepBuffer:1,attribution,pane:pane||'tilePane'});
  const satellite=tile('World_Imagery','Imagery © <a href="https://goto.arcgisonline.com/maps/World_Imagery" target="_blank" rel="noopener">Esri</a>, Vantor, Earthstar Geographics, GIS User Community');
  const streets=tile('World_Street_Map','Streets: '+credits+', USGS, Intermap, NRCan and the GIS User Community');
  const roadPane=map.getPane('road-context')||map.createPane('road-context');roadPane.style.zIndex=350;roadPane.style.pointerEvents='none';
  const roads=tile('Reference/World_Transportation','Roads: '+credits,'road-context');
  const places=tile('Reference/World_Boundaries_and_Places','Labels: '+credits+', GIS User Community','road-context');
  const tiles=[satellite,streets,roads,places];
  for(const layer of tiles)layer.on('tileerror',()=>{if(map.hasLayer(layer))options.onWarning?.('Map tiles could not load. State outlines and search results still work; try another background.');});
  const borderPane=map.getPane('state-context')||map.createPane('state-context');borderPane.style.zIndex=475;borderPane.style.pointerEvents='none';
  let code=options.selectedState||'',outline,halo,attributed=false;
  const borderGroup=L.layerGroup();
  const selected=feature=>feature.properties.code===code||feature.properties.name===code;
  const lineStyle=feature=>({color:selected(feature)?'#ffd56a':'#d8e7ec',weight:selected(feature)?2.8:1.3,opacity:1,fill:false,interactive:false,pane:'state-context'});
  const haloStyle=feature=>({...lineStyle(feature),color:'#15232d',weight:selected(feature)?5:3.4,opacity:.85});
  function setBorders(data){
    halo=L.geoJSON(data,{style:haloStyle,interactive:false}).addTo(borderGroup);
    outline=L.geoJSON(data,{style:lineStyle,interactive:false}).addTo(borderGroup);
    apply();
  }
  function apply(){
    options.onWarning?.('');
    const wanted=new Set(preferences.base==='streets'?[streets]:preferences.base==='satellite'?(preferences.roads?[satellite,roads,places]:[satellite]):[]);
    for(const layer of tiles){if(wanted.has(layer)){if(!map.hasLayer(layer))layer.addTo(map);}else if(map.hasLayer(layer))map.removeLayer(layer);}
    if(preferences.borders){borderGroup.addTo(map);if(!attributed)map.attributionControl?.addAttribution('State outlines: US Census Bureau');}
    else {map.removeLayer(borderGroup);if(attributed)map.attributionControl?.removeAttribution('State outlines: US Census Bureau');}
    attributed=preferences.borders;
  }
  apply();
  if(options.boundaries)setBorders(options.boundaries);
  else fetch('/assets/state-borders.json').then(r=>{if(!r.ok)throw new Error('boundary request failed');return r.json();}).then(setBorders).catch(()=>options.onWarning?.('State outlines could not load. Reload the page to try again.'));
  return {
    preferences,
    setBase(value){preferences.base=value;save();apply();},
    setRoads(value){preferences.roads=value;save();apply();},
    setBorders(value){preferences.borders=value;save();apply();},
    highlightState(value){code=value||'';outline?.setStyle(lineStyle);halo?.setStyle(haloStyle);}
  };
};
