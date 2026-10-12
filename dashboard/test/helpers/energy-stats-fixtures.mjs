// Antwort von GET /api/v1/energy/stats fuer einen Tag mit zwei Buckets.
// local ist die Ortszeit des Pi, start die UTC-Zeit. Absichtlich
// verschieden, damit Tests merken, wenn jemand start beschriftet.
export const DAY_FIXTURE = {
  period: 'day', at: '2026-10-12', prev: '2026-10-11', next: '',
  buckets: [
    {start: '2026-10-12T12:00:00Z', local: '2026-10-12T14:00', kwh: {pv: 2.5, grid_import: 0.1, grid_export: 1.2, load_total: 1.4, 'custom:sauna': 0.8}, hours: {ha: 1, own: 0, none: 0}, autarky: 0.93, self_consumption: 0.52},
    {start: '2026-10-12T13:00:00Z', local: '2026-10-12T15:00', kwh: {}, hours: {ha: 0, own: 0, none: 1}, autarky: null, self_consumption: null},
  ],
  total: {kwh: {pv: 2.5, grid_import: 0.1, grid_export: 1.2, load_total: 1.4, 'custom:sauna': 0.8}, hours: {ha: 20, own: 3, none: 1}, autarky: 0.93, self_consumption: 0.52},
  categories: {sauna: {label: 'Sauna', base: 'consumer', color: 'cat_2'}},
  status: {state: 'recording', mode: 'always'},
};
