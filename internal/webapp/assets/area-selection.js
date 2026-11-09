// Rectangle input for the existing geographic search bounds. Supports a drag
// or two separate corner taps without depending on a drawing plugin or tiles.
window.PinpointAreaSelection = (map, {onSelect, onClear}) => {
  const $ = id => document.getElementById(id);
  const container = map.getContainer();
  let selection, preview, anchorMarker, anchor, pointer, down, secondCorner;
  let drawing = false, restore = [], suppressClicksUntil = 0;

  function setBounds(bounds) {
    if (selection) map.removeLayer(selection);
    selection = bounds ? L.rectangle([[bounds.south, bounds.west], [bounds.north, bounds.east]], {
      color: '#8ce0ca', weight: 2, fillOpacity: .10, interactive: false,
    }).addTo(map) : null;
    $('clear-area').disabled = !selection;
    if (!drawing) $('draw-area').textContent = selection ? 'Redraw rectangle' : 'Draw rectangle';
  }

  function stop() {
    if (drawing) suppressClicksUntil = performance.now() + 400;
    drawing = false;
    const captured = pointer;
    pointer = null;
    if (captured != null && container.hasPointerCapture(captured)) container.releasePointerCapture(captured);
    for (const handler of restore) handler.enable();
    restore = [];
    if (preview) map.removeLayer(preview);
    if (anchorMarker) map.removeLayer(anchorMarker);
    preview = anchorMarker = anchor = null;
    container.classList.remove('drawing-area');
    $('draw-area').textContent = selection ? 'Redraw rectangle' : 'Draw rectangle';
    $('draw-area').setAttribute('aria-pressed', 'false');
    $('cancel-area').hidden = true;
    $('area-instruction').textContent = 'Draw a rectangle to limit the search. Drag the map to pan.';
  }

  function start() {
    stop();
    drawing = true;
    restore = ['dragging', 'boxZoom', 'doubleClickZoom', 'touchZoom', 'scrollWheelZoom', 'keyboard', 'tapHold']
      .map(name => map[name]).filter(handler => handler?.enabled());
    for (const handler of restore) handler.disable();
    container.classList.add('drawing-area');
    $('draw-area').textContent = 'Drawing rectangle…';
    $('draw-area').setAttribute('aria-pressed', 'true');
    $('cancel-area').hidden = false;
    $('area-instruction').textContent = 'Drag a rectangle, or tap two opposite corners. Escape cancels.';
  }

  function point(event) {
    const box = container.getBoundingClientRect();
    const size = map.getSize();
    const xy = L.point(Math.max(0, Math.min(size.x, event.clientX - box.left)), Math.max(0, Math.min(size.y, event.clientY - box.top)));
    const ll = map.containerPointToLatLng(xy);
    return L.latLng(Math.max(-84, Math.min(84, ll.lat)), Math.max(-179, Math.min(179, ll.lng)));
  }

  function showPreview(corner) {
    const bounds = L.latLngBounds(anchor, corner);
    if (preview) preview.setBounds(bounds);
    else preview = L.rectangle(bounds, {color: '#8ce0ca', weight: 2, dashArray: '6 5', fillOpacity: .15, interactive: false}).addTo(map);
  }

  function finish(corner) {
    const a = map.latLngToContainerPoint(anchor), b = map.latLngToContainerPoint(corner);
    if (Math.abs(a.x - b.x) < 6 || Math.abs(a.y - b.y) < 6) {
      $('area-instruction').textContent = 'Choose the opposite corner to give the rectangle a width and height.';
      return;
    }
    const bounds = L.latLngBounds(anchor, corner);
    const selected = {south: bounds.getSouth(), north: bounds.getNorth(), west: bounds.getWest(), east: bounds.getEast()};
    stop();
    onSelect(selected);
  }

  container.addEventListener('pointerdown', event => {
    if (!drawing || event.button !== 0 || event.isPrimary === false || pointer != null || event.target.closest('.leaflet-control')) return;
    event.preventDefault();
    event.stopPropagation();
    pointer = event.pointerId;
    down = L.point(event.clientX, event.clientY);
    secondCorner = !!anchor;
    if (!anchor) {
      anchor = point(event);
      anchorMarker = L.circleMarker(anchor, {radius: 4, color: '#fff', fillColor: '#8ce0ca', fillOpacity: 1, interactive: false}).addTo(map);
    }
    container.setPointerCapture(pointer);
  }, true);
  container.addEventListener('pointermove', event => {
    if (!drawing || !anchor || pointer != null && event.pointerId !== pointer) return;
    event.preventDefault();
    event.stopPropagation();
    showPreview(point(event));
  }, true);
  container.addEventListener('pointerup', event => {
    if (!drawing || event.pointerId !== pointer) return;
    event.preventDefault();
    event.stopPropagation();
    const dragged = down.distanceTo(L.point(event.clientX, event.clientY)) >= 6;
    pointer = null;
    if (container.hasPointerCapture(event.pointerId)) container.releasePointerCapture(event.pointerId);
    if (secondCorner || dragged) finish(point(event));
    else $('area-instruction').textContent = 'Tap the opposite corner to finish the rectangle. Escape cancels.';
  }, true);
  for (const type of ['pointercancel', 'lostpointercapture']) container.addEventListener(type, event => {
    if (event.pointerId === pointer) stop();
  });
  // The finishing touch can synthesize a click/double tap after pointerup,
  // when navigation has already been restored. Keep it from zooming the map.
  for (const type of ['click', 'dblclick']) container.addEventListener(type, event => {
    if (drawing || performance.now() < suppressClicksUntil) {
      event.preventDefault();
      event.stopPropagation();
    }
  }, true);
  window.addEventListener('keydown', event => { if (drawing && event.key === 'Escape') { event.preventDefault(); stop(); } });
  $('draw-area').onclick = () => drawing ? stop() : start();
  $('cancel-area').onclick = stop;
  $('clear-area').onclick = () => { stop(); onClear(); };
  return {setBounds, cancel: stop, isDrawing: () => drawing};
};
