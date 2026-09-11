(function () {
    'use strict';

    const money = new Intl.NumberFormat('en-US', {
        style: 'currency',
        currency: 'USD',
        minimumFractionDigits: 2,
        maximumFractionDigits: 2
    });
    const whole = new Intl.NumberFormat('en-US');

    function requestIdentifier() {
        if (window.crypto && typeof window.crypto.randomUUID === 'function') {
            return window.crypto.randomUUID();
        }
        const bytes = new Uint8Array(16);
        window.crypto.getRandomValues(bytes);
        return Array.from(bytes, function (value) { return value.toString(16).padStart(2, '0'); }).join('');
    }

    function initSpendingOptimizer(root) {
        if (!root || root.dataset.spendingReady === 'true') return;
        root.dataset.spendingReady = 'true';

        const form = root.querySelector('#spending-optimizer-form');
        if (!form) return;
        const run = root.querySelector('#spending-optimizer-run');
        const cancel = root.querySelector('#spending-optimizer-cancel');
        const status = root.querySelector('#spending-optimizer-status');
        const results = root.querySelector('#spending-optimizer-results');
        const minimum = root.querySelector('#spending-minimum');
        const minimumNote = root.querySelector('#spending-minimum-note');
        const range = root.querySelector('[data-spending-range]');
        const boostEnabled = root.querySelector('#spending-boost-enabled');
        const boostAmount = root.querySelector('#spending-boost-amount');
        const boostStop = root.querySelector('#spending-boost-stop');
        const boostBoundary = root.querySelector('#spending-boost-boundary');
        const boostRequired = root.querySelector('#spending-boost-required');

        let requestID = '';
        let responseGeneration = 0;
        let responseController = null;
        let applyController = null;
        let prepareGeneration = 0;
        let prepareController = null;
        let prepareTimer = 0;
        let graphGeneration = 0;
        let graphController = null;
        let graphRenderGeneration = 0;
        let graphPayload = null;
        let graphOrigin = null;
        let cleaned = false;
        let themeObserver = null;
        let sizeObserver = null;

        function setBusy(value) {
            run.disabled = value;
            cancel.disabled = !value;
            results.setAttribute('aria-busy', String(value));
        }

        function setBoostState() {
            const enabled = boostEnabled.checked;
            boostAmount.disabled = !enabled;
            boostStop.disabled = !enabled;
            boostAmount.required = enabled;
            boostStop.required = enabled;
            boostRequired.hidden = !enabled;
            if (!enabled) {
                boostAmount.setCustomValidity('');
                boostStop.setCustomValidity('');
                boostBoundary.textContent = '';
                return;
            }
            const start = root.dataset.planStart;
            const end = root.dataset.planEnd;
            if (boostStop.value && start && boostStop.value <= start) {
                boostBoundary.textContent = 'Choose a stop month after the plan start month.';
            } else if (boostStop.value && end && boostStop.value > end) {
                boostBoundary.textContent = 'This planned expiry is outside the modeled horizon and will not appear in the modeled timeline.';
            } else {
                boostBoundary.textContent = 'The stop month is the first month without the extra amount.';
            }
        }

        function purgeGraph() {
            graphGeneration++;
            graphRenderGeneration++;
            if (graphController) graphController.abort();
            graphController = null;
            graphPayload = null;
            const panel = root.querySelector('#spending-graph-preview');
            const plot = root.querySelector('#spending-graph-main');
            if (plot && window.Plotly) {
                try { window.Plotly.purge(plot); } catch (_) { /* an unrendered node is already clear */ }
            }
            if (plot) plot.replaceChildren();
            if (panel) {
                panel.hidden = true;
                panel.querySelector('#spending-graph-heading').textContent = '';
                panel.querySelector('#spending-graph-summary').textContent = '';
                panel.querySelector('#spending-graph-note').textContent = '';
                panel.querySelector('#spending-graph-data').replaceChildren();
            }
            root.querySelectorAll('[data-spending-graph]').forEach(button => button.setAttribute('aria-pressed', 'false'));
            graphOrigin = null;
        }

        function cancelRetainedPreview() {
            if (!requestID) return;
            const canceledID = requestID;
            requestID = '';
            fetch('/whatif/spending/optimize/cancel', {
                method: 'POST',
                body: new URLSearchParams({request_id: canceledID}),
                keepalive: true
            }).catch(function () {});
        }

        function invalidate(message, cancelServer, prepareScheduled) {
            responseGeneration++;
            prepareGeneration++;
            graphGeneration++;
            graphRenderGeneration++;
            if (responseController) responseController.abort();
            if (applyController) applyController.abort();
            if (prepareController) prepareController.abort();
            if (graphController) graphController.abort();
            responseController = null;
            applyController = null;
            prepareController = null;
            graphController = null;
            clearTimeout(prepareTimer);
            purgeGraph();
            results.replaceChildren();
            results.setAttribute('aria-busy', 'false');
            setBusy(false);
            range.textContent = prepareScheduled ? 'Validating the edited inputs\u2026' : 'Range validation is idle. Compare again to revalidate the exact range and step.';
            minimumNote.textContent = '';
            if (cancelServer !== false) cancelRetainedPreview();
            status.textContent = message || '';
        }

        function formBody() {
            return new URLSearchParams(new FormData(form));
        }

        async function prepare(showErrors) {
            const generation = ++prepareGeneration;
            if (prepareController) prepareController.abort();
            prepareController = null;
            if (!minimum.value || !minimum.validity.valid) {
                range.textContent = 'Enter your minimum to see the exact server-checked range and step.';
                if (showErrors) {
                    status.textContent = 'Enter a positive monthly minimum before comparing.';
                    minimum.focus();
                    minimum.reportValidity();
                }
                return false;
            }
            if (!form.checkValidity()) {
                if (showErrors) {
                    status.textContent = 'Correct the first highlighted field before comparing.';
                    form.querySelector(':invalid')?.focus();
                    form.reportValidity();
                }
                return false;
            }
            const controller = new AbortController();
            prepareController = controller;
            try {
                const response = await fetch('/whatif/spending/optimize/prepare', {
                    method: 'POST',
                    body: formBody(),
                    signal: controller.signal
                });
                if (generation !== prepareGeneration || !root.isConnected) return false;
                if (!response.ok) {
                    const message = (await response.text()).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
                    throw new Error(message || 'The search range could not be prepared.');
                }
                const payload = await response.json();
                if (generation !== prepareGeneration || !root.isConnected) return false;
                const normalized = payload.request;
                range.textContent = money.format(normalized.search_min_monthly_real) + ' to ' + money.format(normalized.search_max_monthly_real) + ', in ' + money.format(normalized.search_step_monthly_real) + ' steps.';
                minimumNote.textContent = payload.minimum_note || '';
                return true;
            } catch (error) {
                if (generation !== prepareGeneration || !root.isConnected || error.name === 'AbortError') return false;
                range.textContent = 'The server could not validate this range.';
                if (showErrors) {
                    status.textContent = error.message || 'The search range could not be prepared.';
                    minimum.focus();
                }
                return false;
            } finally {
                if (generation === prepareGeneration) prepareController = null;
            }
        }

        function schedulePrepare() {
            clearTimeout(prepareTimer);
            prepareTimer = setTimeout(function () { prepare(false); }, 220);
        }

        async function applyOption(applyForm) {
            const button = applyForm.querySelector('button[type="submit"]');
            const generation = responseGeneration;
            if (applyController) applyController.abort();
            const controller = new AbortController();
            applyController = controller;
            button.disabled = true;
            status.textContent = 'Applying this spending option…';
            try {
                const response = await fetch(applyForm.getAttribute('action'), {
                    method: 'POST',
                    body: new URLSearchParams(new FormData(applyForm)),
                    signal: controller.signal
                });
                if (generation !== responseGeneration || !root.isConnected) return;
                if (!response.ok) {
                    const message = (await response.text()).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
                    throw new Error(message || 'This option could not be applied.');
                }
                requestID = '';
                status.textContent = 'Spending option saved. Refreshing the projection.';
                const redirect = response.headers && response.headers.get('HX-Redirect');
                if (redirect) window.location.assign(redirect);
            } catch (error) {
                if (generation !== responseGeneration || !root.isConnected || error.name === 'AbortError') return;
                status.textContent = error.message || 'This option could not be applied. Nothing was saved.';
                button.focus();
            } finally {
                if (applyController === controller) applyController = null;
                if (generation === responseGeneration && root.isConnected && !cleaned) button.disabled = false;
            }
        }

        function theme() {
            const dark = document.documentElement.classList.contains('dark');
            return {
                text: dark ? '#e7e5e4' : '#44403c',
                grid: dark ? '#a8a29e' : '#6b7280',
                blue: dark ? '#93c5fd' : '#1d4ed8',
                blueSoft: dark ? 'rgba(147,197,253,.20)' : 'rgba(29,78,216,.14)',
                red: dark ? '#f87171' : '#dc2626',
                green: dark ? '#4ade80' : '#15803d',
                amber: dark ? '#fcd34d' : '#92400e',
                planned: dark ? '#d6d3d1' : '#57534e'
            };
        }

        function chartLayout(title, yTitle, years = []) {
            const colors = theme();
            const narrow = Math.min(results.clientWidth, window.innerWidth) < 430;
            const width = Math.max(260, Math.min(results.clientWidth - 26, window.innerWidth - 26, 760));
            const margin = {l: narrow ? 58 : 72, r: narrow ? 8 : 18, t: narrow ? 76 : 58, b: narrow ? 155 : 105};
            // Thin labels only: retain every year in each trace and values table.
            // Integer steps also avoid duplicate rounded year labels on short plans.
            const maxYearTicks = Math.max(2, Math.min(10, Math.floor((width - margin.l - margin.r) / 60)));
            const yearSpan = years.length ? Math.max(...years) - Math.min(...years) : 0;
            const yearStep = Math.max(1, Math.ceil(yearSpan / (maxYearTicks - 1)));
            return {
                title: {text: title, font: {size: narrow ? 13 : 15}},
                autosize: false,
                width,
                height: narrow ? 455 : 410,
                paper_bgcolor: 'rgba(0,0,0,0)',
                plot_bgcolor: 'rgba(0,0,0,0)',
                font: {family: 'system-ui, -apple-system, sans-serif', color: colors.text},
                margin,
                legend: {orientation: 'h', y: narrow ? -0.38 : -0.24, font: {size: narrow ? 10 : 12}},
                xaxis: {title: {text: 'Plan year'}, gridcolor: colors.grid, automargin: true, tick0: years.length ? Math.min(...years) : 0, dtick: yearStep, tickformat: 'd', tickangle: 0},
                yaxis: {title: {text: yTitle}, gridcolor: colors.grid, automargin: true, tickprefix: '$', rangemode: 'tozero'},
                transition: {duration: 0}
            };
        }

        function makeTable(captionText, headers, rows) {
            const table = document.createElement('table');
            table.className = 'w-full min-w-max text-left text-xs';
            const caption = table.createCaption();
            caption.className = 'p-2 text-left font-medium';
            caption.textContent = captionText;
            const header = table.createTHead().insertRow();
            headers.forEach(function (label) {
                const cell = document.createElement('th');
                cell.scope = 'col';
                cell.className = 'p-2';
                cell.textContent = label;
                header.append(cell);
            });
            const body = table.createTBody();
            rows.forEach(function (values) {
                const row = body.insertRow();
                row.className = 'border-t border-gray-300 dark:border-gray-600';
                values.forEach(function (value) {
                    const cell = row.insertCell();
                    cell.className = 'p-2';
                    cell.textContent = String(value);
                });
            });
            return table;
        }

        function simulatedView(payload, measure) {
            const series = payload.simulated;
            const colors = theme();
            const living = measure === 'spending';
            const p10 = living ? series.LivingP10 : series.PortfolioP10;
            const p50 = living ? series.LivingP50 : series.PortfolioP50;
            const p90 = living ? series.LivingP90 : series.PortfolioP90;
            const label = living ? 'Funded monthly living — annual averages' : 'Portfolio — plan-year end';
            const lower = {x: series.years, y: p10, name: 'P10', type: 'scatter', mode: 'lines', line: {color: colors.blue, width: 1.5}};
            const upper = {x: series.years, y: p90, name: 'P90', type: 'scatter', mode: 'lines', line: {color: colors.blue, width: 1.5}, fill: 'tonexty', fillcolor: colors.blueSoft};
            const median = {x: series.years, y: p50, name: 'Median (P50)', type: 'scatter', mode: 'lines', line: {color: colors.blue, width: 3}};
            const traces = [lower, upper, median];
            if (living && payload.display_dollars === 'real' && series.floor !== undefined) {
                traces.push({x: series.years, y: series.years.map(function () { return series.floor; }), name: 'Comfortable monthly minimum', type: 'scatter', mode: 'lines', line: {color: colors.amber, dash: 'dash', width: 2}});
            }
            const rows = series.years.map(function (year, index) {
                const row = [year, whole.format((series.path_counts || [])[index] || 0), money.format(p10[index]), money.format(p50[index]), money.format(p90[index])];
                if (living && payload.display_dollars === 'real' && series.floor !== undefined) row.push(money.format(series.floor));
                return row;
            });
            const headers = ['Plan year', 'Observed futures', 'P10', 'Median (P50)', 'P90'];
            if (living && payload.display_dollars === 'real' && series.floor !== undefined) headers.push('Monthly minimum');
            return {
                traces,
                layout: chartLayout(label, payload.display_dollars === 'real' ? "Today's dollars" : 'Nominal dollars', series.years),
                summary: living ? 'Median, P10, and P90 funded monthly living averaged within each plan year.' : 'Median, P10, and P90 portfolio values at each plan-year end.',
                note: (series.floor_note || '') + (living ? ' Annual averages can hide an individual month below the minimum; qualification checks every month.' : ''),
                table: makeTable(label + ' — ' + (payload.display_dollars === 'real' ? "today's dollars" : 'nominal dollars'), headers, rows)
            };
        }

        function worstView(payload) {
            const series = payload.worst;
            const colors = theme();
            const traces = [{x: series.years, y: series.LivingP50, name: 'One coherent observed future', type: 'scatter', mode: 'lines', line: {color: colors.red, width: 3}}];
            if (payload.display_dollars === 'real' && series.floor !== undefined) {
                traces.push({x: series.years, y: series.years.map(function () { return series.floor; }), name: 'Comfortable monthly minimum', type: 'scatter', mode: 'lines', line: {color: colors.amber, dash: 'dash', width: 2}});
            }
            const metrics = payload.candidate.metrics || {};
            const exact = money.format(metrics.lowest_observed_monthly_real);
            const month = metrics.lowest_observed_month;
            const rows = series.years.map(function (year, index) {
                return [year, money.format(series.LivingP50[index]), payload.display_dollars === 'real' && series.floor !== undefined ? money.format(series.floor) : 'Omitted in nominal view'];
            });
            return {
                traces,
                layout: chartLayout('Lowest observed living path — one simulated future', payload.display_dollars === 'real' ? "Today's dollars" : 'Nominal dollars', series.years),
                summary: 'In this retained future, the exact lowest monthly funded living was ' + exact + " in today's dollars in plan month " + month + '.',
                note: (series.floor_note || '') + ' This is one coherent observed path. It is not a point-by-point stitched lower band or an exhaustive worst case.',
                table: makeTable('Lowest observed future — annual average funded monthly living (' + (payload.display_dollars === 'real' ? "today's dollars" : 'nominal dollars') + ')', ['Plan year', 'Funded monthly living', 'Monthly minimum'], rows)
            };
        }

        function timelineView(payload) {
            const timeline = payload.funding_timeline;
            if (!timeline || !timeline.annual_averages) throw new Error('The base-case funding timeline is unavailable.');
            const colors = theme();
            const years = timeline.annual_averages.map(function (row) { return row.calendar_year; });
            const value = function (key) { return timeline.annual_averages.map(function (row) { return row[key]; }); };
            const traces = [
                {x: years, y: value('taxes_paid_real'), name: 'Taxes paid', type: 'scatter', mode: 'lines', line: {color: colors.red, width: 2}},
                {x: years, y: value('social_security_real'), name: 'Social Security', type: 'scatter', mode: 'lines', line: {color: colors.green, width: 2}},
                {x: years, y: value('other_configured_income_real'), name: 'Other configured income', type: 'scatter', mode: 'lines', line: {color: colors.amber, width: 2}},
                {x: years, y: value('tax_deferred_withdrawal_real'), name: 'Tax-deferred withdrawal', type: 'scatter', mode: 'lines', line: {color: colors.blue, width: 2}},
                {x: years, y: value('taxable_withdrawal_real'), name: 'Taxable withdrawal', type: 'scatter', mode: 'lines', line: {color: colors.amber, width: 2, dash: 'dot'}},
                {x: years, y: value('roth_withdrawal_real'), name: 'Roth withdrawal', type: 'scatter', mode: 'lines', line: {color: colors.green, width: 2, dash: 'dot'}},
                {x: years, y: value('planned_living_real'), name: 'Planned living', type: 'scatter', mode: 'lines', line: {color: colors.planned, width: 2, dash: 'dash'}},
                {x: years, y: value('funded_living_real'), name: 'Funded living', type: 'scatter', mode: 'lines', line: {color: colors.blue, width: 3}}
            ];
            const narrow = Math.min(results.clientWidth, window.innerWidth) < 430;
            const timelineTitle = narrow ? timeline.title.replace(' \u2014 ', '<br>') : timeline.title;
            const layout = chartLayout(timelineTitle, "Average monthly amount in today's dollars", years);
            layout.xaxis.title.text = 'Calendar year';
            const markerX = function (calendarMonth) {
                const parts = calendarMonth.split('-').map(Number);
                return parts[0] + (parts[1] - 1) / 12;
            };
            layout.shapes = (timeline.markers || []).map(function (marker) {
                return {type: 'line', x0: markerX(marker.calendar_month), x1: markerX(marker.calendar_month), y0: 0, y1: 1, yref: 'paper', line: {color: colors.amber, dash: 'dot', width: 1}};
            });
            const annotationGroups = [];
            (timeline.markers || []).forEach(function (marker) {
                const existing = annotationGroups.find(function (group) { return group.calendarMonth === marker.calendar_month; });
                if (existing) existing.labels.push(marker.label);
                else annotationGroups.push({calendarMonth: marker.calendar_month, labels: [marker.label]});
            });
            layout.annotations = annotationGroups.map(function (group, index) {
                return {x: markerX(group.calendarMonth), y: 1, yref: 'paper', text: group.labels.join('<br>'), showarrow: false, textangle: -45, yshift: index % 2 ? -8 : 6, font: {size: 10}};
            });
            const rows = timeline.annual_averages.map(function (row) {
                return [row.calendar_year + (row.partial ? ' (partial)' : ''), row.observed_months, money.format(row.social_security_real), money.format(row.other_configured_income_real), money.format(row.tax_deferred_withdrawal_real), money.format(row.taxable_withdrawal_real), money.format(row.roth_withdrawal_real), money.format(row.taxes_paid_real), money.format(row.planned_living_real), money.format(row.funded_living_real), money.format(row.healthcare_real)];
            });
            const fragment = document.createDocumentFragment();
            const monthlyRows = (timeline.months || []).map(function (row) {
                return [row.calendar_month, money.format(row.social_security_real), money.format(row.other_configured_income_real), money.format(row.tax_deferred_withdrawal_real), money.format(row.taxable_withdrawal_real), money.format(row.roth_withdrawal_real), money.format(row.taxes_paid_real), money.format(row.planned_living_real), money.format(row.funded_living_real), money.format(row.healthcare_real)];
            });
            fragment.append(makeTable("Observed calendar-month amounts — today's dollars", ['Calendar month', 'Social Security', 'Other configured income', 'Tax-deferred withdrawal', 'Taxable withdrawal', 'Roth withdrawal', 'Taxes paid', 'Planned living', 'Funded living', 'Healthcare'], monthlyRows));
            fragment.append(makeTable("Calendar-year average monthly amounts — today's dollars", ['Calendar year', 'Observed months', 'Social Security', 'Other configured income', 'Tax-deferred withdrawal', 'Taxable withdrawal', 'Roth withdrawal', 'Taxes paid', 'Planned living', 'Funded living', 'Healthcare'], rows));
            if ((timeline.markers || []).length) {
                fragment.append(makeTable('Observed configured events', ['Calendar month', 'Event'], timeline.markers.map(function (marker) { return [marker.calendar_month, marker.label]; })));
            }
            return {
                traces,
                layout,
                summary: 'Base-case income, gross account withdrawals, taxes paid, and living spending from ' + timeline.start_month + ' through ' + timeline.observed_end_month + '.',
                note: [timeline.accounting_note, timeline.coverage_note, timeline.availability_note].filter(Boolean).join(' '),
                table: fragment
            };
        }

        function baseView(payload) {
            const chart = structuredClone(payload.base_case_chart || {data: [], layout: {}});
            const colors = theme();
            if (typeof window.applyTonePalette === 'function' && typeof window.getTonePalette === 'function') {
                window.applyTonePalette(chart.data, window.getTonePalette());
            }
            chart.layout = Object.assign(chartLayout(payload.base_case_label, payload.display_dollars === 'real' ? "Today's dollars" : 'Nominal dollars'), chart.layout || {});
            const rows = [];
            (chart.data || []).forEach(function (trace) {
                (trace.x || []).forEach(function (x, index) { rows.push([trace.name || 'Series', x, money.format(trace.y[index])]); });
            });
            return {
                traces: chart.data || [],
                layout: chart.layout,
                summary: payload.base_case_label + '.',
                note: 'This is one canonical base case under configured assumptions, separate from the simulated ranges.',
                table: makeTable('Base-case chart values', ['Series', 'Time', 'Value'], rows)
            };
        }

        function viewFor(payload, view) {
            if (view === 'worst') return worstView(payload);
            if (view === 'timeline') return timelineView(payload);
            if (view === 'portfolio') return simulatedView(payload, 'portfolio');
            if (view === 'base') return baseView(payload);
            return simulatedView(payload, 'spending');
        }

        async function renderGraph() {
            if (!graphPayload || !root.isConnected) return;
            if (!window.Plotly) throw new Error('Chart library is unavailable. Reload the page and try again.');
            const panel = root.querySelector('#spending-graph-preview');
            const oldPlot = panel.querySelector('#spending-graph-main');
            const viewSelect = panel.querySelector('#spending-graph-view');
            const dollarsSelect = panel.querySelector('#spending-graph-dollars');
            dollarsSelect.disabled = viewSelect.value === 'timeline';
            const rendered = viewFor(graphPayload, viewSelect.value);
            const revision = ++graphRenderGeneration;
            const plot = oldPlot.cloneNode(false);
            plot.style.width = rendered.layout.width + 'px';
            await window.Plotly.newPlot(plot, rendered.traces, rendered.layout, {responsive: true, displayModeBar: false, staticPlot: true});
            if (revision !== graphRenderGeneration || !root.isConnected || !graphPayload) {
                window.Plotly.purge(plot);
                return;
            }
            try { window.Plotly.purge(oldPlot); } catch (_) {}
            plot.style.width = '100%';
            oldPlot.replaceWith(plot);
            panel.querySelector('#spending-graph-summary').textContent = rendered.summary;
            panel.querySelector('#spending-graph-note').textContent = rendered.note;
            panel.querySelector('#spending-graph-data').replaceChildren(rendered.table);
            panel.hidden = false;
            window.Plotly.Plots.resize(plot);
        }

        async function loadGraph(button, mode) {
            const generation = ++graphGeneration;
            const searchGeneration = responseGeneration;
            if (graphController) graphController.abort();
            const controller = new AbortController();
            graphController = controller;
            status.textContent = 'Loading spending evidence…';
            try {
                const response = await fetch('/whatif/spending/optimize/graph', {
                    method: 'POST',
                    body: new URLSearchParams({
                        request_id: button.dataset.requestId,
                        candidate: button.dataset.graphToken,
                        display_dollars: mode || 'real'
                    }),
                    signal: controller.signal
                });
                const payload = await response.json();
                if (generation !== graphGeneration || searchGeneration !== responseGeneration || !root.isConnected) return;
                if (!response.ok) throw new Error(payload.error || 'The graph could not be loaded.');
                graphPayload = payload;
                graphOrigin = button;
                root.querySelectorAll('[data-spending-graph]').forEach(function (item) {
                    item.setAttribute('aria-pressed', String(item === button));
                });
                const panel = root.querySelector('#spending-graph-preview');
                panel.querySelector('#spending-graph-heading').textContent = 'Evidence for ' + (payload.candidate_label || 'selected spending option');
                panel.querySelector('#spending-graph-dollars').value = payload.display_dollars;
                await renderGraph();
                if (generation !== graphGeneration || searchGeneration !== responseGeneration || !root.isConnected) return;
                status.textContent = 'Evidence loaded. Nothing was saved.';
                panel.querySelector('#spending-graph-heading').focus({preventScroll: true});
                panel.scrollIntoView({block: 'start', behavior: 'auto'});
            } catch (error) {
                if (generation !== graphGeneration || searchGeneration !== responseGeneration || !root.isConnected || error.name === 'AbortError') return;
                status.textContent = error.message || 'The graph could not be loaded.';
                button.focus();
            } finally {
                if (generation === graphGeneration) graphController = null;
            }
        }

        form.addEventListener('input', function () {
            setBoostState();
            invalidate('Inputs changed. Previous previews and Apply actions were cleared.', true, true);
            schedulePrepare();
        });
        boostEnabled.addEventListener('change', setBoostState);
        cancel.addEventListener('click', function () {
            invalidate('Comparison cancelled. Nothing was saved.', true, false);
            run.focus();
        });
        form.addEventListener('submit', async function (event) {
            event.preventDefault();
            invalidate('', true, true);
            const prepared = await prepare(true);
            if (!prepared || !root.isConnected) return;
            requestID = requestIdentifier();
            const generation = ++responseGeneration;
            const controller = new AbortController();
            responseController = controller;
            const body = formBody();
            body.set('request_id', requestID);
            setBusy(true);
            status.textContent = 'Comparing spending options across simulated futures. This may take a few minutes.';
            try {
                const response = await fetch('/whatif/spending/optimize', {method: 'POST', body, signal: controller.signal});
                const markup = await response.text();
                if (generation !== responseGeneration || !root.isConnected) return;
                results.innerHTML = markup;
                if (window.htmx) window.htmx.process(results);
                if (!response.ok) {
                    status.textContent = results.textContent.trim() || 'The comparison could not complete. Nothing was saved.';
                    (results.querySelector('[role="alert"]') || results.querySelector('[tabindex="-1"]'))?.focus();
                    return;
                }
                status.textContent = 'Comparison complete. Review the spending options before applying.';
                results.querySelector('[data-spending-outcome]')?.focus();
            } catch (error) {
                if (generation !== responseGeneration || !root.isConnected || error.name === 'AbortError') return;
                status.textContent = 'The comparison could not complete. Run it again. Nothing was saved.';
                run.focus();
            } finally {
                if (generation === responseGeneration && root.isConnected) {
                    responseController = null;
                    setBusy(false);
                }
            }
        });

        root.addEventListener('submit', function (event) {
            const applyForm = event.target.closest('[data-spending-apply-form]');
            if (!applyForm) return;
            event.preventDefault();
            applyOption(applyForm);
        });
        root.addEventListener('click', function (event) {
            const graphButton = event.target.closest('[data-spending-graph]');
            if (graphButton) {
                loadGraph(graphButton, 'real');
                return;
            }
            if (event.target.closest('#spending-graph-close')) {
                const origin = graphOrigin;
                purgeGraph();
                status.textContent = 'Evidence closed. Nothing was saved.';
                origin?.focus();
            }
        });
        root.addEventListener('change', function (event) {
            if (event.target.id === 'spending-graph-view' && graphPayload) {
                renderGraph().catch(function (error) {
                    status.textContent = error.message || 'The selected evidence view could not be displayed.';
                });
            }
            if (event.target.id === 'spending-graph-dollars' && graphOrigin) {
                loadGraph(graphOrigin, event.target.value);
            }
        });

        const handleHTMXBeforeRequest = function (event) {
            if (!root.isConnected) return;
            const target = event.detail.elt;
            if (target && root.contains(target)) return;
            if (event.detail.requestConfig && event.detail.requestConfig.verb !== 'get') {
                invalidate('Plan inputs changed. Previous spending previews were cleared.', true, false);
            }
        };
        const handleHTMXAfterSwap = function (event) {
            if (!root.isConnected) return;
            if (event.detail.target === results) {
                const error = results.querySelector('[role="alert"]');
                const outcome = results.querySelector('[data-spending-outcome]');
                (error || outcome)?.focus();
            }
        };

        const refreshTheme = function () {
            if (cleaned || !root.isConnected || !graphPayload) return;
            renderGraph().catch(function () {});
        };
        const handleHTMXCleanup = function (event) {
            const removed = event.detail && event.detail.elt;
            if (cleaned || !removed || (removed !== root && !removed.contains(root))) return;
            cleaned = true;
            responseGeneration++;
            prepareGeneration++;
            graphGeneration++;
            graphRenderGeneration++;
            clearTimeout(prepareTimer);
            if (responseController) responseController.abort();
            if (applyController) applyController.abort();
            if (prepareController) prepareController.abort();
            if (graphController) graphController.abort();
            responseController = null;
            applyController = null;
            prepareController = null;
            graphController = null;
            cancelRetainedPreview();
            purgeGraph();
            themeObserver.disconnect();
            sizeObserver.disconnect();
            window.removeEventListener('themechange', refreshTheme);
            document.body.removeEventListener('htmx:beforeRequest', handleHTMXBeforeRequest);
            document.body.removeEventListener('htmx:afterSwap', handleHTMXAfterSwap);
            document.body.removeEventListener('htmx:beforeCleanupElement', handleHTMXCleanup);
        };
        document.body.addEventListener('htmx:beforeRequest', handleHTMXBeforeRequest);
        document.body.addEventListener('htmx:afterSwap', handleHTMXAfterSwap);
        document.body.addEventListener('htmx:beforeCleanupElement', handleHTMXCleanup);
        window.addEventListener('themechange', refreshTheme);
        themeObserver = new MutationObserver(refreshTheme);
        themeObserver.observe(document.documentElement, {attributes: true, attributeFilter: ['class']});
        sizeObserver = new ResizeObserver(function () {
            const plot = root.querySelector('#spending-graph-main');
            if (root.isConnected && plot && plot.data && window.Plotly) window.Plotly.Plots.resize(plot);
        });
        sizeObserver.observe(results);

        setBoostState();
        if (minimum.value) schedulePrepare();
    }

    function initialize(container) {
        if (container && container.matches && container.matches('#spending-optimizer')) initSpendingOptimizer(container);
        (container || document).querySelectorAll?.('#spending-optimizer').forEach(initSpendingOptimizer);
    }

    initialize(document);
    document.addEventListener('DOMContentLoaded', function () { initialize(document); }, {once: true});
    if (!window.__spendingOptimizerHTMXReady) {
        window.__spendingOptimizerHTMXReady = true;
        document.body.addEventListener('htmx:afterSwap', function (event) { initialize(event.detail.target); });
    }
}());
