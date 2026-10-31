import { useEffect, useRef, useState } from 'react';
import maplibregl, { type GeoJSONSource, type SymbolLayerSpecification } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';
import { useApi } from '../../api/client';
import { Icon, ErrorText } from '../../components/Common';
import type { Entity, Navigation } from '../../../shared/contracts';
import { matchesEntity, type Filters } from '../companies/Filters';
import {searchCompanies} from '../companies/companySearch';
import { officeFeatures, metroFeatures } from './geometry';
import { MapSearch } from './MapSearch';
import mcd3 from '../../../src/mcd3.json';
import line2 from '../../../src/zamoskvoretskaya.json';
import line10 from '../../../src/lyublinsko-dmitrovskaya.json';
import line3 from '../../../src/arbatsko-pokrovskaya.json';
export function MapLegend() {
    return <div className="legend">
        <span><i className="office-key"/>офисы</span>
        <span><i className="metro-key"/>метро и МЦК</span>
        <span><span className="metro-lines-key">{[line2, mcd3, line10, line3].map(route =>
            <i key={route.code} style={{ background: route.color }}/>)}</span>линии 2, D3, 10 и 3</span>
        <span><a href="https://openfreemap.org/" target="_blank" rel="noreferrer">© OpenFreeMap</a> · <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">© OpenStreetMap</a></span>
    </div>;
}
export default function MapSection({ nav, filters, query, onCount, selectedCompany, onQueryChange }: {
    nav: Navigation;
    selectedCompany?: string;
    filters: Filters;
    query: string;
    onQueryChange?: (query: string) => void;
    onCount: (n: number) => void;
}) { const container = useRef<HTMLDivElement>(null), map = useRef<maplibregl.Map | null>(null), navRef = useRef(nav); navRef.current = nav; const q = useApi<{
    companies: Entity[];
}>('/api/companies/map'), [ready, setReady] = useState(false), [error, setError] = useState<unknown>(), [metro, setMetro] = useState(true), [lines, setLines] = useState(true); const filteredEntities = (q.data?.companies || []).filter(e => matchesEntity(e, filters)); const entities = searchCompanies(filteredEntities, query); useEffect(() => onCount(entities.length), [entities.length, onCount]); useEffect(() => { if (!container.current)
    return; let disposed = false; const instance = new maplibregl.Map({ container: container.current, style: 'https://tiles.openfreemap.org/styles/bright', center: [37.6176, 55.7558], zoom: 10.7, minZoom: 9, maxZoom: 18, attributionControl: false, renderWorldCopies: false }); map.current = instance;const resize = () => { if (!disposed) instance.resize(); };const observer=new ResizeObserver(resize);observer.observe(container.current);const resizeFrame=requestAnimationFrame(resize); instance.on('error', e => { if (!disposed)
    setError(e.error); }); instance.on('load', () => { const routes = [mcd3, line2, line10, line3]; instance.addSource('metro-lines', { type: 'geojson', data: { type: 'FeatureCollection', features: routes.map(line => ({ type: 'Feature', properties: { color: line.color }, geometry: { type: 'LineString', coordinates: line.stations.map(s => [s.o, s.a]) } })) } }); instance.addLayer({ id: 'metro-outline', type: 'line', source: 'metro-lines', paint: { 'line-color': '#fff', 'line-width': 10, 'line-opacity': .5 }, layout: { 'line-cap': 'round', 'line-join': 'round' } }); instance.addLayer({ id: 'metro-line', type: 'line', source: 'metro-lines', paint: { 'line-color': ['get', 'color'], 'line-width': 6, 'line-opacity': .5 }, layout: { 'line-cap': 'round', 'line-join': 'round' } }); instance.addSource('metro', { type: 'geojson', data: { type: 'FeatureCollection', features: [] } }); instance.addLayer({ id: 'metro-dots', type: 'circle', source: 'metro', paint: { 'circle-radius': ['case', ['get', 'transfer'], 8, 6], 'circle-color': ['get', 'color'], 'circle-stroke-width': 2, 'circle-stroke-color': '#fff' } }); instance.addLayer({ id: 'metro-label', type: 'symbol', source: 'metro', minzoom: 11.2, layout: { 'text-field': ['get', 'label'], 'text-font': ['Open Sans Regular'], 'text-size': 10, 'text-offset': [0, 1.15], 'text-anchor': 'top' }, paint: { 'text-color': '#334155', 'text-halo-color': 'rgba(255,255,255,.94)', 'text-halo-width': 1.5 } }); instance.addSource('companies', { type: 'geojson', data: { type: 'FeatureCollection', features: [] }, cluster: true, clusterRadius: 64, clusterMaxZoom: 14 }); const canvas = document.createElement('canvas'); canvas.width = 64; canvas.height = 40; const context = canvas.getContext('2d'); if (context) {
    context.fillStyle = 'rgba(71,85,105,.96)';
    context.beginPath();
    context.roundRect(2, 2, 60, 36, 7);
    context.fill();
    instance.addImage('company-label-bg', context.getImageData(0, 0, 64, 40), { pixelRatio: 2, stretchX: [[16, 48]], stretchY: [[12, 28]], content: [4, 4, 60, 36] });
} instance.addLayer({ id: 'company-clusters', type: 'circle', source: 'companies', filter: ['has', 'point_count'], paint: { 'circle-radius': ['step', ['get', 'point_count'], 15, 10, 18, 25, 22], 'circle-color': '#2563eb', 'circle-stroke-width': 2, 'circle-stroke-color': '#fff' } }); instance.addLayer({ id: 'company-count', type: 'symbol', source: 'companies', filter: ['has', 'point_count'], layout: { 'text-field': ['get', 'point_count_abbreviated'], 'text-font': ['Open Sans Bold'], 'text-size': 12, 'text-allow-overlap': true, 'text-ignore-placement': true }, paint: { 'text-color': '#fff' } }); instance.addLayer({ id: 'company-dots', type: 'circle', source: 'companies', filter: ['!', ['has', 'point_count']], paint: { 'circle-radius': ['case', ['get', 'selected'], 8, 6], 'circle-color': '#2563eb', 'circle-stroke-width': ['case', ['get', 'selected'], 3, 2], 'circle-stroke-color': '#fff' } }); for (const side of ['left', 'right'] as const) {
    const layout: SymbolLayerSpecification['layout'] = { 'text-field': ['get', 'label'], 'text-font': ['Open Sans Regular'], 'text-size': 12.5, 'text-max-width': 100, 'icon-image': 'company-label-bg', 'icon-text-fit': 'both', 'icon-text-fit-padding': [3, 7, 3, 7], 'text-allow-overlap': true, 'icon-allow-overlap': true, 'text-optional': false, 'icon-optional': false, 'text-ignore-placement': true, 'icon-ignore-placement': true, 'text-anchor': side === 'right' ? 'left' : 'right', 'text-offset': [side === 'right' ? 1.7 : -1.7, 0], 'icon-anchor': side === 'right' ? 'left' : 'right', 'icon-offset': [side === 'right' ? 1.7 : -1.7, 0] };
    instance.addLayer({ id: 'company-label-' + side, type: 'symbol', source: 'companies', filter: ['all', ['!', ['has', 'point_count']], ['==', ['get', 'labelSide'], side]], layout, paint: { 'text-color': '#fff' } });
} for (const id of ['company-clusters', 'company-count', 'company-dots', 'company-label-left', 'company-label-right']) {
    instance.on('mouseenter', id, () => instance.getCanvas().style.cursor = 'pointer');
    instance.on('mouseleave', id, () => instance.getCanvas().style.cursor = '');
    instance.on('click', id, async (e) => { const props = e.features?.[0]?.properties; if (props?.cluster_id != null) {
        const zoom = await (instance.getSource('companies') as GeoJSONSource).getClusterExpansionZoom(Number(props.cluster_id));
        if (!disposed)
            instance.easeTo({ center: e.lngLat, zoom: Math.min(zoom, 18), duration: 260 });
    }
    else if (props?.companyName)
        navRef.current.openCompany(String(props.companyName), 'company'); });
} setReady(true); }); return () => { disposed = true; cancelAnimationFrame(resizeFrame); observer.disconnect(); instance.remove(); map.current = null; }; }, []); useEffect(() => { if (!ready || !map.current)
    return; (map.current.getSource('companies') as GeoJSONSource).setData(officeFeatures(entities, selectedCompany)); }, [ready, q.data, filters, selectedCompany]); useEffect(() => { if (!ready || !map.current)
    return; (map.current.getSource('metro') as GeoJSONSource).setData(metroFeatures(entities)); }, [ready, q.data, filters]); useEffect(() => { if (!ready || !map.current)
    return; for (const id of ['metro-dots', 'metro-label'])
    map.current.setLayoutProperty(id, 'visibility', metro ? 'visible' : 'none'); for (const id of ['metro-outline', 'metro-line'])
    map.current.setLayoutProperty(id, 'visibility', lines ? 'visible' : 'none'); }, [ready, metro, lines]); return <><div className="map" aria-label="Интерактивная карта офисов Москвы"><div id="maplibreMap" ref={container}/></div>{(q.loading || q.error || error) && <div className="map-status">{q.loading ? 'Загрузка карты…' : <ErrorText error={q.error || error}/>}</div>}<MapSearch query={query} entities={filteredEntities} nav={nav} onQueryChange={onQueryChange}/><div className="controls"><button className="control" aria-label="Приблизить" onClick={() => map.current?.zoomIn()}><Icon name="plus"/></button><button className="control" aria-label="Отдалить" onClick={() => map.current?.zoomOut()}><Icon name="minus"/></button><button className="control reset" aria-label="Показать всю Москву" onClick={() => map.current?.easeTo({ center: [37.6176, 55.7558], zoom: 10.7, bearing: 0, pitch: 0, duration: 260 })}><Icon name="maximize"/></button><button className={'control metro-control' + (metro ? ' active' : '')} aria-label="Показать или скрыть станции метро" aria-pressed={metro} onClick={() => setMetro(!metro)}>М</button><button className={'control metro-lines-control' + (lines ? ' active' : '')} aria-label="Показать или скрыть линии метро" aria-pressed={lines} onClick={() => setLines(!lines)}>Л</button></div><MapLegend/></>; }
