package datatable

import (
	"fmt"
	"strings"
)

// Generate produces a self-contained HTML file from a TableDef.
// The output uses React.createElement (no JSX, no Babel) loaded via CDN.
// Data is embedded inline or loaded via fetch/SSE/WebSocket.
func Generate(td *TableDef, jsonData string) string {
	var sb strings.Builder

	sb.WriteString(genHeader(td))
	sb.WriteString(genStyles(td))
	sb.WriteString(genScriptTags())
	sb.WriteString(genBodyOpen(td))
	sb.WriteString(genDataScript(td, jsonData))
	sb.WriteString(genReactApp(td))
	sb.WriteString(genFooter())

	return sb.String()
}

func genHeader(td *TableDef) string {
	bg := "#0f1117"
	if td.Theme == "light" {
		bg = "#ffffff"
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<meta name="generator" content="datatable-gen">
<style>
body { margin: 0; background: %s; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }
* { box-sizing: border-box; }
</style>
`, td.Title, bg)
}

func genStyles(td *TableDef) string {
	isDark := td.Theme == "dark"

	bg := "#0f1117"
	surface := "#1a1d27"
	border := "#2a2d3a"
	text := "#e4e7f0"
	textMuted := "#8b8fa8"
	accent := "#5b6cf9"
	headerBg := "#141720"

	if !isDark {
		bg = "#f4f5f7"
		surface = "#ffffff"
		border = "#e1e4e8"
		text = "#24292e"
		textMuted = "#586069"
		accent = "#0366d6"
		headerBg = "#ffffff"
	}

	return fmt.Sprintf(`<style>
:root {
  --bg: %s;
  --surface: %s;
  --border: %s;
  --text: %s;
  --text-muted: %s;
  --accent: %s;
  --header-bg: %s;
  --green: #22c55e;
  --yellow: #f59e0b;
  --orange: #f97316;
  --red: #ef4444;
  --blue: #3b82f6;
  --purple: #a855f7;
}

#app { min-height: 100vh; background: var(--bg); color: var(--text); }

.navbar {
  background: var(--header-bg);
  border-bottom: 1px solid var(--border);
  padding: 0 24px;
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  position: sticky;
  top: 0;
  z-index: 100;
}
.navbar-title { font-size: 18px; font-weight: 700; color: var(--text); }
.navbar-meta { font-size: 12px; color: var(--text-muted); }
.navbar-status { display: flex; align-items: center; gap: 8px; }
.status-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); }
.status-dot.connecting { background: var(--yellow); animation: pulse 1s infinite; }
.status-dot.error { background: var(--red); }

.main { padding: 24px; }

.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}
.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 16px 20px;
}
.card-label { font-size: 12px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 6px; }
.card-value { font-size: 28px; font-weight: 700; color: var(--text); }
.card-value.green { color: var(--green); }
.card-value.yellow { color: var(--yellow); }
.card-value.red { color: var(--red); }
.card-value.blue { color: var(--blue); }

.table-container {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 10px;
  overflow: hidden;
}
.table-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
  gap: 10px;
}
.search-input {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 7px 12px;
  color: var(--text);
  font-size: 14px;
  width: 260px;
  outline: none;
}
.search-input:focus { border-color: var(--accent); }
.search-input::placeholder { color: var(--text-muted); }

.export-btn {
  background: var(--accent);
  color: white;
  border: none;
  border-radius: 6px;
  padding: 7px 14px;
  font-size: 13px;
  cursor: pointer;
  font-weight: 500;
}
.export-btn:hover { opacity: 0.85; }

.filter-row {
  display: flex;
  gap: 8px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  flex-wrap: wrap;
  align-items: center;
}
.filter-select {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: 5px 10px;
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
}
.filter-label { font-size: 12px; color: var(--text-muted); margin-right: 4px; }

table { width: 100%%; border-collapse: collapse; font-size: 13px; }
th {
  background: var(--header-bg);
  border-bottom: 1px solid var(--border);
  padding: 10px 14px;
  text-align: left;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}
th:hover { color: var(--text); }
th .sort-arrow { margin-left: 4px; opacity: 0.4; }
th .sort-arrow.active { opacity: 1; color: var(--accent); }

td {
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  color: var(--text);
  vertical-align: middle;
}
tr:last-child td { border-bottom: none; }
tr:hover td { background: rgba(255,255,255,0.02); }

.badge {
  display: inline-flex;
  align-items: center;
  padding: 3px 9px;
  border-radius: 20px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.badge-healthy, .badge-valid, .badge-reachable, .badge-200, .badge-ok {
  background: rgba(34,197,94,0.15);
  color: var(--green);
}
.badge-warning, .badge-degraded {
  background: rgba(245,158,11,0.15);
  color: var(--yellow);
}
.badge-down, .badge-expired, .badge-unreachable, .badge-error {
  background: rgba(239,68,68,0.15);
  color: var(--red);
}
.badge-info, .badge-301, .badge-302, .badge-redirect {
  background: rgba(59,130,246,0.15);
  color: var(--blue);
}

.latency-fast { color: var(--green); }
.latency-medium { color: var(--yellow); }
.latency-slow { color: var(--orange); }
.latency-very-slow { color: var(--red); }

.days-ok { color: var(--green); font-weight: 600; }
.days-warning { color: var(--yellow); font-weight: 600; }
.days-critical { color: var(--orange); font-weight: 600; }
.days-expired { color: var(--red); font-weight: 600; }

.pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
  font-size: 13px;
  color: var(--text-muted);
  flex-wrap: wrap;
  gap: 8px;
}
.pagination-btns { display: flex; gap: 4px; }
.page-btn {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: 5px 10px;
  color: var(--text);
  cursor: pointer;
  font-size: 12px;
  min-width: 32px;
  text-align: center;
}
.page-btn:hover { border-color: var(--accent); color: var(--accent); }
.page-btn.active { background: var(--accent); border-color: var(--accent); color: white; }
.page-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.empty-state {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
}
.empty-icon { font-size: 32px; margin-bottom: 12px; }

.loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 48px;
  gap: 12px;
  color: var(--text-muted);
}
.spinner {
  width: 20px; height: 20px;
  border: 2px solid var(--border);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}

@keyframes spin { to { transform: rotate(360deg); } }
@keyframes pulse { 0%%, 100%% { opacity: 1; } 50%% { opacity: 0.4; } }

.source-tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  padding: 3px 8px;
  border-radius: 4px;
  background: rgba(91,108,249,0.12);
  color: var(--accent);
  font-weight: 500;
}
</style>
`, bg, surface, border, text, textMuted, accent, headerBg)
}

func genScriptTags() string {
	return `</head>
<body>
<div id="app"></div>

<!-- React 18 via CDN — no Babel, no JSX -->
<script crossorigin src="https://unpkg.com/react@18/umd/react.production.min.js"></script>
<script crossorigin src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js"></script>
`
}

func genBodyOpen(_ *TableDef) string {
	return ""
}

func genDataScript(td *TableDef, jsonData string) string {
	var sb strings.Builder

	sb.WriteString("<script>\n")

	// Embed static data if provided
	if jsonData != "" {
		// Ensure we have valid JSON — strip markdown fences and preamble text
		cleaned := strings.TrimSpace(jsonData)
		if strings.HasPrefix(cleaned, "```") {
			lines := strings.Split(cleaned, "\n")
			start := 1
			end := len(lines) - 1
			if end > start && strings.TrimSpace(lines[end]) == "```" {
				cleaned = strings.Join(lines[start:end], "\n")
			}
		}
		cleaned = strings.TrimSpace(cleaned)
		// Find the actual JSON — look for first [ or {
		if idx := strings.Index(cleaned, "["); idx > 0 {
			cleaned = cleaned[idx:]
		} else if idx := strings.Index(cleaned, "{"); idx > 0 {
			cleaned = cleaned[idx:]
		}
		// Trim trailing text after the JSON
		if strings.HasPrefix(cleaned, "[") {
			if idx := strings.LastIndex(cleaned, "]"); idx >= 0 {
				cleaned = cleaned[:idx+1]
			}
		} else if strings.HasPrefix(cleaned, "{") {
			if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
				cleaned = cleaned[:idx+1]
			}
		}
		cleaned = strings.TrimSpace(cleaned)
		// Escape </script> inside data to prevent HTML parser breakage
		cleaned = strings.ReplaceAll(cleaned, "</script>", `<\/script>`)
		// Only embed if it looks like JSON
		if len(cleaned) > 0 && (cleaned[0] == '[' || cleaned[0] == '{') {
			sb.WriteString(fmt.Sprintf("window.__STATIC_DATA__ = %s;\n", cleaned))
		} else {
			sb.WriteString(fmt.Sprintf("window.__STATIC_DATA__ = %q;\n", cleaned))
		}
	}

	// Embed table config
	sb.WriteString(fmt.Sprintf(`window.__TABLE_CONFIG__ = {
  title: %q,
  source: { type: %q, url: %q, pollSec: %d },
  dataField: %q,
  theme: %q,
  search: %v,
  pageSize: %d,
  exportCSV: %v,
  columns: [
`,
		td.Title,
		string(td.Source.Type),
		td.Source.URL,
		td.Source.PollSec,
		td.DataField,
		td.Theme,
		td.Search,
		td.PageSize,
		td.ExportCSV,
	))

	for _, col := range td.Columns {
		sb.WriteString(fmt.Sprintf(
			`    { field: %q, label: %q, type: %q, sortable: %v, filterable: %v, color: %q, range: %v },`+"\n",
			col.Field, col.Label, string(col.Type),
			col.Sortable, col.Filterable,
			col.Color, col.Range,
		))
	}

	sb.WriteString("  ]\n};\n</script>\n")
	return sb.String()
}

func genReactApp(td *TableDef) string {
	sourceComment := map[SourceType]string{
		SourceStatic: "📦 static",
		SourceRest:   "🔄 REST",
		SourceSSE:    "📡 SSE",
		SourceWS:     "⚡ WebSocket",
	}[td.Source.Type]

	_ = sourceComment // used in JS template
	_ = td

	return `<script>
(function() {
  var h = React.createElement;
  var cfg = window.__TABLE_CONFIG__;
  var cols = cfg.columns;

  // ── Utilities ──────────────────────────────────────────────────────────

  function getNestedValue(obj, field) {
    var parts = field.split('.');
    var val = obj;
    for (var i = 0; i < parts.length; i++) {
      if (val == null) return '';
      val = val[parts[i]];
    }
    return val == null ? '' : val;
  }

  function badgeClass(val, colorScheme) {
    if (!val) return 'badge';
    var v = String(val).toLowerCase().replace(/[^a-z0-9]/g, '-');
    switch (colorScheme) {
      case 'health':
        if (v === 'healthy' || v === 'valid' || v === 'reachable' || v === '200') return 'badge badge-healthy';
        if (v === 'degraded' || v === 'warning') return 'badge badge-warning';
        if (v === 'down' || v === 'expired' || v === 'unreachable') return 'badge badge-down';
        return 'badge badge-info';
      case 'status':
        if (v === 'valid' || v === 'reachable' || v === 'ok') return 'badge badge-healthy';
        if (v === 'warning') return 'badge badge-warning';
        if (v === 'expired' || v === 'error' || v === 'unreachable') return 'badge badge-down';
        return 'badge badge-info';
      default:
        var code = parseInt(val);
        if (code >= 200 && code < 300) return 'badge badge-healthy';
        if (code >= 300 && code < 400) return 'badge badge-info';
        if (code >= 400) return 'badge badge-down';
        if (v === 'healthy' || v === 'valid' || v === 'reachable') return 'badge badge-healthy';
        if (v === 'warning' || v === 'degraded') return 'badge badge-warning';
        if (v === 'error' || v === 'down' || v === 'expired') return 'badge badge-down';
        return 'badge badge-info';
    }
  }

  function latencyClass(ms) {
    var n = parseInt(ms);
    if (isNaN(n)) return '';
    if (n < 100) return 'latency-fast';
    if (n < 300) return 'latency-medium';
    if (n < 1000) return 'latency-slow';
    return 'latency-very-slow';
  }

  function daysClass(days) {
    var n = parseInt(days);
    if (isNaN(n) || n < 0) return 'days-expired';
    if (n < 7) return 'days-critical';
    if (n < 30) return 'days-warning';
    return 'days-ok';
  }

  function renderCell(col, row) {
    var val = getNestedValue(row, col.field);
    var display = val === null || val === undefined ? '—' : String(val);

    if (col.type === 'badge') {
      return h('td', { key: col.field },
        h('span', { className: badgeClass(val, col.color) }, display)
      );
    }
    if (col.type === 'number') {
      var cls = '';
      if (col.color === 'latency') cls = latencyClass(val);
      if (col.field.indexOf('days') >= 0 || col.field.indexOf('remaining') >= 0) cls = daysClass(val);
      return h('td', { key: col.field },
        h('span', { className: cls }, display)
      );
    }
    if (col.type === 'link' && display !== '—') {
      return h('td', { key: col.field },
        h('a', { href: display, target: '_blank', style: { color: 'var(--accent)', textDecoration: 'none' } }, display)
      );
    }
    return h('td', { key: col.field }, display);
  }

  function exportCSV(rows) {
    var header = cols.map(function(c) { return JSON.stringify(c.label); }).join(',');
    var lines = rows.map(function(row) {
      return cols.map(function(c) {
        var v = getNestedValue(row, c.field);
        return JSON.stringify(v === null || v === undefined ? '' : v);
      }).join(',');
    });
    var csv = [header].concat(lines).join('\n');
    var blob = new Blob([csv], { type: 'text/csv' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'export.csv';
    a.click();
    URL.revokeObjectURL(url);
  }

  // ── Summary Cards ──────────────────────────────────────────────────────

  function buildCards(rows) {
    var total = rows.length;
    var healthy = 0, degraded = 0, down = 0;
    rows.forEach(function(row) {
      var h = (getNestedValue(row, 'overall_health') || getNestedValue(row, 'health') || '').toLowerCase();
      if (h === 'healthy' || h === 'ok') healthy++;
      else if (h === 'degraded' || h === 'warning') degraded++;
      else if (h === 'down' || h === 'error') down++;
      else healthy++; // default
    });
    return [
      { label: 'Total', value: total, cls: 'blue' },
      { label: 'Healthy', value: healthy, cls: 'green' },
      { label: 'Degraded', value: degraded, cls: 'yellow' },
      { label: 'Down', value: down, cls: 'red' },
    ];
  }

  // ── Data Loading ───────────────────────────────────────────────────────

  function loadData(setData, setStatus) {
    var src = cfg.source;
    var field = cfg.dataField;

    function extract(obj) {
      if (field && obj[field]) return obj[field];
      if (Array.isArray(obj)) return obj;
      // try common wrappers
      var wrappers = ['sites', 'data', 'rows', 'items', 'results'];
      for (var i = 0; i < wrappers.length; i++) {
        if (obj[wrappers[i]]) return obj[wrappers[i]];
      }
      return obj;
    }

    if (src.type === 'static') {
      var raw = window.__STATIC_DATA__;
      if (raw) {
        setData(extract(raw));
        setStatus('ok');
      } else {
        setStatus('empty');
      }
      return;
    }

    if (src.type === 'rest') {
      setStatus('loading');
      function doFetch() {
        fetch(src.url)
          .then(function(r) { return r.json(); })
          .then(function(json) { setData(extract(json)); setStatus('ok'); })
          .catch(function() { setStatus('error'); });
      }
      doFetch();
      if (src.pollSec > 0) {
        setInterval(doFetch, src.pollSec * 1000);
      }
      return;
    }

    if (src.type === 'sse') {
      setStatus('connecting');
      var es = new EventSource(src.url);
      es.onopen = function() { setStatus('ok'); };
      es.onmessage = function(e) {
        try { setData(extract(JSON.parse(e.data))); setStatus('ok'); } catch(err) {}
      };
      es.onerror = function() { setStatus('error'); };
      return;
    }

    if (src.type === 'ws') {
      setStatus('connecting');
      var ws = new WebSocket(src.url);
      ws.onopen = function() { setStatus('ok'); };
      ws.onmessage = function(e) {
        try { setData(extract(JSON.parse(e.data))); setStatus('ok'); } catch(err) {}
      };
      ws.onerror = function() { setStatus('error'); };
      ws.onclose = function() { setStatus('error'); };
      return;
    }
  }

  // ── Main App Component ─────────────────────────────────────────────────

  function App() {
    var stateData = React.useState([]);
    var data = stateData[0]; var setData = stateData[1];

    var stateStatus = React.useState('loading');
    var status = stateStatus[0]; var setStatus = stateStatus[1];

    var stateSearch = React.useState('');
    var search = stateSearch[0]; var setSearch = stateSearch[1];

    var stateFilters = React.useState({});
    var filters = stateFilters[0]; var setFilters = stateFilters[1];

    var stateSort = React.useState({ field: null, dir: 'asc' });
    var sort = stateSort[0]; var setSort = stateSort[1];

    var statePage = React.useState(1);
    var page = statePage[0]; var setPage = statePage[1];

    var stateCheckedAt = React.useState('');
    var checkedAt = stateCheckedAt[0]; var setCheckedAt = stateCheckedAt[1];

    React.useEffect(function() {
      loadData(function(rows) {
        setData(rows);
        setCheckedAt(new Date().toLocaleString());
      }, setStatus);
    }, []);

    // Filtering
    var filtered = data.filter(function(row) {
      if (search) {
        var s = search.toLowerCase();
        var match = cols.some(function(c) {
          var v = String(getNestedValue(row, c.field) || '').toLowerCase();
          return v.indexOf(s) >= 0;
        });
        if (!match) return false;
      }
      return Object.keys(filters).every(function(field) {
        var fv = filters[field];
        if (!fv) return true;
        var rv = String(getNestedValue(row, field) || '').toLowerCase();
        return rv === fv.toLowerCase();
      });
    });

    // Sorting
    var sorted = filtered.slice().sort(function(a, b) {
      if (!sort.field) return 0;
      var av = getNestedValue(a, sort.field);
      var bv = getNestedValue(b, sort.field);
      var na = parseFloat(av); var nb = parseFloat(bv);
      var cmp = isNaN(na) || isNaN(nb)
        ? String(av || '').localeCompare(String(bv || ''))
        : na - nb;
      return sort.dir === 'asc' ? cmp : -cmp;
    });

    // Pagination
    var pageSize = cfg.pageSize;
    var totalPages = Math.max(1, Math.ceil(sorted.length / pageSize));
    var paginated = sorted.slice((page - 1) * pageSize, page * pageSize);

    function handleSort(field) {
      setSort(function(prev) {
        return prev.field === field && prev.dir === 'asc'
          ? { field: field, dir: 'desc' }
          : { field: field, dir: 'asc' };
      });
      setPage(1);
    }

    function handleFilter(field, val) {
      setFilters(function(prev) {
        var next = Object.assign({}, prev);
        next[field] = val;
        return next;
      });
      setPage(1);
    }

    // Unique values for filter dropdowns
    function uniqueValues(field) {
      var seen = {};
      var vals = [];
      data.forEach(function(row) {
        var v = String(getNestedValue(row, field) || '');
        if (v && !seen[v]) { seen[v] = true; vals.push(v); }
      });
      return vals.sort();
    }

    // Status dot class
    var dotCls = 'status-dot';
    if (status === 'connecting') dotCls += ' connecting';
    if (status === 'error') dotCls += ' error';

    // Source label
    var sourceLabels = { static: '📦 static', rest: '🔄 REST', sse: '📡 SSE', ws: '⚡ WebSocket' };
    var sourceLabel = sourceLabels[cfg.source.type] || cfg.source.type;

    var cards = buildCards(data);

    return h('div', { id: 'app-inner' },

      // Navbar
      h('div', { className: 'navbar' },
        h('div', { className: 'navbar-title' }, cfg.title),
        h('div', { className: 'navbar-status' },
          h('span', { className: 'source-tag' }, sourceLabel),
          h('div', { className: dotCls }),
          h('span', { className: 'navbar-meta' },
            status === 'ok' ? (checkedAt ? 'Updated ' + checkedAt : 'Live') :
            status === 'loading' ? 'Loading…' :
            status === 'connecting' ? 'Connecting…' :
            status === 'error' ? 'Connection error' : status
          )
        )
      ),

      // Main content
      h('div', { className: 'main' },

        // Summary cards
        h('div', { className: 'cards' },
          cards.map(function(c) {
            return h('div', { key: c.label, className: 'card' },
              h('div', { className: 'card-label' }, c.label),
              h('div', { className: 'card-value ' + c.cls }, c.value)
            );
          })
        ),

        // Table container
        h('div', { className: 'table-container' },

          // Toolbar
          h('div', { className: 'table-toolbar' },
            cfg.search
              ? h('input', {
                  className: 'search-input',
                  placeholder: '🔍  Search all columns…',
                  value: search,
                  onChange: function(e) { setSearch(e.target.value); setPage(1); }
                })
              : null,
            h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } },
              h('span', { style: { fontSize: '13px', color: 'var(--text-muted)' } },
                sorted.length + ' / ' + data.length + ' rows'
              ),
              cfg.exportCSV
                ? h('button', { className: 'export-btn', onClick: function() { exportCSV(sorted); } }, '↓ Export CSV')
                : null
            )
          ),

          // Column filters row
          h('div', { className: 'filter-row' },
            h('span', { className: 'filter-label' }, 'Filter:'),
            cols.filter(function(c) { return c.filterable; }).map(function(col) {
              var vals = uniqueValues(col.field);
              return h('select', {
                key: col.field,
                className: 'filter-select',
                value: filters[col.field] || '',
                onChange: function(e) { handleFilter(col.field, e.target.value); }
              },
                h('option', { value: '' }, col.label + ' — all'),
                vals.map(function(v) { return h('option', { key: v, value: v }, v); })
              );
            })
          ),

          // Table
          status === 'loading' || status === 'connecting'
            ? h('div', { className: 'loading' },
                h('div', { className: 'spinner' }),
                h('span', null, status === 'connecting' ? 'Connecting to ' + cfg.source.url + '…' : 'Loading data…')
              )
            : status === 'error'
              ? h('div', { className: 'empty-state' },
                  h('div', { className: 'empty-icon' }, '⚠️'),
                  h('div', null, 'Failed to load data from ' + cfg.source.url)
                )
              : paginated.length === 0
                ? h('div', { className: 'empty-state' },
                    h('div', { className: 'empty-icon' }, '🔍'),
                    h('div', null, 'No rows match your filters')
                  )
                : h('table', null,
                    h('thead', null,
                      h('tr', null,
                        cols.map(function(col) {
                          var isActive = sort.field === col.field;
                          var arrow = isActive ? (sort.dir === 'asc' ? ' ▲' : ' ▼') : ' ⇅';
                          return h('th', {
                            key: col.field,
                            onClick: col.sortable ? function() { handleSort(col.field); } : null,
                            style: col.sortable ? { cursor: 'pointer' } : {}
                          },
                            col.label,
                            col.sortable
                              ? h('span', { className: 'sort-arrow' + (isActive ? ' active' : '') }, arrow)
                              : null
                          );
                        })
                      )
                    ),
                    h('tbody', null,
                      paginated.map(function(row, i) {
                        return h('tr', { key: i },
                          cols.map(function(col) { return renderCell(col, row); })
                        );
                      })
                    )
                  ),

          // Pagination
          h('div', { className: 'pagination' },
            h('span', null,
              'Page ' + page + ' of ' + totalPages +
              ' (' + sorted.length + ' rows)'
            ),
            h('div', { className: 'pagination-btns' },
              h('button', {
                className: 'page-btn',
                disabled: page <= 1,
                onClick: function() { setPage(1); }
              }, '«'),
              h('button', {
                className: 'page-btn',
                disabled: page <= 1,
                onClick: function() { setPage(function(p) { return p - 1; }); }
              }, '‹'),
              // Page number buttons — show window of 5
              (function() {
                var btns = [];
                var start = Math.max(1, page - 2);
                var end = Math.min(totalPages, start + 4);
                for (var i = start; i <= end; i++) {
                  (function(n) {
                    btns.push(h('button', {
                      key: n,
                      className: 'page-btn' + (n === page ? ' active' : ''),
                      onClick: function() { setPage(n); }
                    }, String(n)));
                  })(i);
                }
                return btns;
              })(),
              h('button', {
                className: 'page-btn',
                disabled: page >= totalPages,
                onClick: function() { setPage(function(p) { return p + 1; }); }
              }, '›'),
              h('button', {
                className: 'page-btn',
                disabled: page >= totalPages,
                onClick: function() { setPage(totalPages); }
              }, '»')
            )
          )
        )
      )
    );
  }

  // Mount
  var container = document.getElementById('app');
  var root = ReactDOM.createRoot(container);
  root.render(h(App, null));

})();
</script>
`
}

func genFooter() string {
	return `</body>
</html>`
}
