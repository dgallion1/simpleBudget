// What-If Rate Assumptions card: person rows (add/remove/derived age),
// spending-decline preview panel, phase-reference dropdown. Extracted
// from components/whatif/rate-assumptions.html (U7).

function personRows() {
    return Array.from(document.querySelectorAll('[data-person-row]'));
}

function hasSpousePerson() {
    return personRows().some((row) => row.querySelector('[data-person-role-input]')?.value === 'spouse');
}

function updatePersonRole(select) {
    const row = select.closest('[data-person-row]');
    const hidden = row?.querySelector('[data-person-role-input]');
    if (hidden) {
        hidden.value = select.value;
    }
    togglePhaseReferenceDropdown();
}

function removePersonRow(button) {
    const row = button.closest('[data-person-row]');
    if (row) {
        row.remove();
    }
    togglePhaseReferenceDropdown();
    updatePersonAgePreviews();
}

function computeDerivedAge(startDate, birthMonth) {
    if (!startDate || !birthMonth) {
        return 'Age -';
    }
    const [startYear, startMonth] = startDate.split('-').map(Number);
    const [birthYear, birthMonthNum] = birthMonth.split('-').map(Number);
    if (!startYear || !startMonth || !birthYear || !birthMonthNum) {
        return 'Age -';
    }
    const months = (startYear - birthYear) * 12 + (startMonth - birthMonthNum);
    if (months < 0) {
        return 'Age -';
    }
    return 'Age ' + Math.floor(months / 12);
}

function updatePersonAgePreviews() {
    const startDate = document.getElementById('projection-start-date')?.value || '';
    personRows().forEach((row) => {
        const birthMonth = row.querySelector('input[name="person_birth_month[]"]')?.value || '';
        const preview = row.querySelector('[data-person-age-preview]');
        if (preview) {
            preview.textContent = computeDerivedAge(startDate, birthMonth);
        }
    });
}

function addPersonRow() {
    const container = document.getElementById('person-rows');
    if (!container) {
        return;
    }

    const role = hasSpousePerson() ? 'other' : 'spouse';
    const row = document.createElement('div');
    row.className = 'grid grid-cols-12 gap-2 items-end rounded-md border border-gray-200 dark:border-gray-600 p-2';
    row.setAttribute('data-person-row', '');
    row.innerHTML = `
        <input type="hidden" name="person_id[]" value="">
        <input type="hidden" name="person_role[]" value="${role}" data-person-role-input>
        <div class="col-span-4">
            <span class="block text-xs text-gray-600 dark:text-gray-400">Name</span>
            <input type="text" name="person_name[]" value="" aria-label="Person name"
                class="mt-1 block w-full rounded-md border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-accent focus:ring-accent text-sm">
        </div>
        <div class="col-span-3">
            <span class="block text-xs text-gray-600 dark:text-gray-400">Birth Month</span>
            <input type="month" name="person_birth_month[]" value="" aria-label="Person birth month"
                onchange="updatePersonAgePreviews()"
                class="mt-1 block w-full rounded-md border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-accent focus:ring-accent text-sm">
        </div>
        <div class="col-span-2">
            <span class="block text-xs text-gray-600 dark:text-gray-400">Role</span>
            <select data-person-role-selector onchange="updatePersonRole(this)" aria-label="Person role"
                class="mt-1 block w-full rounded-md border border-gray-300 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 shadow-sm focus:border-accent focus:ring-accent text-sm">
                <option value="spouse" ${role === 'spouse' ? 'selected' : ''}>Spouse</option>
                <option value="other" ${role === 'other' ? 'selected' : ''}>Other</option>
            </select>
        </div>
        <div class="col-span-2">
            <span class="block text-xs text-gray-600 dark:text-gray-400">Derived Age</span>
            <div class="mt-1 rounded-md bg-gray-100 px-3 py-2 text-sm text-gray-700 dark:bg-gray-600 dark:text-gray-100" data-person-age-preview>Age -</div>
        </div>
        <div class="col-span-1 flex justify-end">
            <button type="button" data-remove-person-row
                class="text-xs text-negative hover:text-negative">Remove</button>
        </div>
    `;
    container.appendChild(row);
    togglePhaseReferenceDropdown();
    updatePersonAgePreviews();
}

// Toggle phase reference dropdown visibility based on spouse person rows
function togglePhaseReferenceDropdown() {
    const container = document.getElementById('phase-age-reference-container');
    const select = document.getElementById('phase-age-reference-select');
    if (container) {
        if (hasSpousePerson()) {
            container.classList.remove('hidden');
        } else {
            container.classList.add('hidden');
            if (select && select.value === 'spouse') {
                select.value = 'older';
            }
        }
    }
}

function updateTaxablePercent() {
    const taxDeferred = parseFloat(document.querySelector('input[name="tax_deferred_percent"]').value) || 0;
    const roth = parseFloat(document.querySelector('input[name="roth_percent"]').value) || 0;
    const taxable = Math.max(0, 100 - taxDeferred - roth);
    const display = document.getElementById('taxable-percent-display');
    if (display) {
        display.textContent = Math.round(taxable) + '%';
        // Warn if over 100%
        if (taxDeferred + roth > 100) {
            display.classList.add('text-negative', 'text-negative');
            display.textContent = 'Over 100%!';
        } else {
            display.classList.remove('text-negative', 'text-negative');
        }
    }
    // Show/hide per-account allocation sections based on portfolio split
    const tdSection = document.getElementById('td-alloc-section');
    const rothSection = document.getElementById('roth-alloc-section');
    if (tdSection) tdSection.classList.toggle('hidden', taxDeferred <= 0);
    if (rothSection) rothSection.classList.toggle('hidden', roth <= 0);
    // Update dollar amounts display
    updateAccountAmounts();
}

function updateAccountAmounts() {
    // Get portfolio value from the slider
    const portfolioSlider = document.querySelector('input[name="portfolio_value"]');
    if (!portfolioSlider) return;
    const portfolioValue = parseFloat(portfolioSlider.value) || 0;

    // Get account percentages
    const tdPercent = parseFloat(document.querySelector('input[name="tax_deferred_percent"]')?.value) || 0;
    const rothPercent = parseFloat(document.querySelector('input[name="roth_percent"]')?.value) || 0;
    const taxablePercent = Math.max(0, 100 - tdPercent - rothPercent);

    // Calculate and display amounts
    const formatAmount = (amount) => {
        if (amount === 0) return '$0';
        if (amount >= 1000000) return '$' + (amount / 1000000).toFixed(1) + 'M';
        if (amount >= 1000) return '$' + Math.round(amount / 1000) + 'K';
        return '$' + Math.round(amount);
    };

    const tdAmount = portfolioValue * (tdPercent / 100);
    const rothAmount = portfolioValue * (rothPercent / 100);
    const taxableAmount = portfolioValue * (taxablePercent / 100);

    const tdDisplay = document.getElementById('td-amount-display');
    const rothDisplay = document.getElementById('roth-amount-display');
    const taxableDisplay = document.getElementById('taxable-amount-display');

    if (tdDisplay) tdDisplay.textContent = formatAmount(tdAmount);
    if (rothDisplay) rothDisplay.textContent = formatAmount(rothAmount);
    if (taxableDisplay) taxableDisplay.textContent = formatAmount(taxableAmount);
}

// Initialize account amounts on page load
document.addEventListener('DOMContentLoaded', updateAccountAmounts);
document.addEventListener('DOMContentLoaded', updatePersonAgePreviews);
document.addEventListener('DOMContentLoaded', togglePhaseReferenceDropdown);

function updateAccountBonds(account) {
    const prefixMap = {
        'td': 'tax_deferred',
        'roth': 'roth',
        'taxable': 'taxable'
    };
    const prefix = prefixMap[account];
    const stockInput = document.querySelector(`input[name="${prefix}_stock_percent"]`);
    const cashInput = document.querySelector(`input[name="${prefix}_cash_percent"]`);
    let stocks = parseFloat(stockInput.value) || 0;
    let cash = parseFloat(cashInput.value) || 0;

    // Clamp: stocks + cash cannot exceed 100%
    if (stocks + cash > 100) {
        // Reduce whichever field was just changed (the event target)
        const target = document.activeElement;
        if (target === cashInput) {
            cash = 100 - stocks;
            cashInput.value = Math.max(0, cash);
        } else {
            stocks = 100 - cash;
            stockInput.value = Math.max(0, stocks);
        }
    }

    const bonds = Math.max(0, 100 - stocks - cash);
    const display = document.getElementById(`${account}-bond-display`);
    if (display) {
        display.textContent = 'Bonds: ' + Math.round(bonds) + '%';
        display.classList.remove('text-negative', 'text-negative');
    }
    // Update expected return if using allocation-based returns
    const returnSlider = document.querySelector('input[name="investment_return"]');
    if (returnSlider && parseFloat(returnSlider.value) === 0) {
        updateInvestmentReturnDisplay(0);
    }
}

// Legacy function for backwards compatibility
function updateBondPercent() {
    // No longer used - per-account allocation now
}

function updateInvestmentReturnDisplay(value) {
    const val = parseFloat(value);
    const displays = document.querySelectorAll('[data-quick-adjust-display="investment_return"]');
    displays.forEach((display) => {
        const inPanel = display.closest('#quick-adjust-panel') !== null;
        // Clear existing content
        display.textContent = '';
        if (val === 0) {
            const expectedReturn = calculateExpectedReturnFromAllocation();
            const label = document.createElement('span');
            label.className = inPanel ? 'text-positive' : 'text-positive';
            label.textContent = 'Using asset allocation';
            const detail = document.createElement('span');
            detail.className = inPanel ? 'text-gray-500' : 'text-gray-600 dark:text-gray-400';
            detail.textContent = '(~' + expectedReturn.toFixed(1) + '% expected)';
            display.appendChild(label);
            display.appendChild(document.createTextNode(' '));
            display.appendChild(detail);
        } else {
            const label = document.createElement('span');
            label.className = inPanel ? 'text-gray-200' : 'text-gray-600 dark:text-gray-300';
            label.textContent = 'Fixed ' + val.toFixed(1) + '%';
            const detail = document.createElement('span');
            detail.className = inPanel ? 'text-warning text-body-sm' : 'text-warning text-body-sm';
            detail.textContent = '(overrides allocation)';
            display.appendChild(label);
            display.appendChild(document.createTextNode(' '));
            display.appendChild(detail);
        }
    });
}

function calculateExpectedReturnFromAllocation() {
    // Conservative forward-looking estimates (more prudent for retirement planning)
    const stockMean = 7.0;
    const bondMean = 4.0;
    const cashMean = 3.0;

    // Get account weights
    const tdPercent = parseFloat(document.querySelector('input[name="tax_deferred_percent"]')?.value) || 60;
    const rothPercent = parseFloat(document.querySelector('input[name="roth_percent"]')?.value) || 10;
    const taxPercent = Math.max(0, 100 - tdPercent - rothPercent);

    // Get per-account allocations (default 60/40/0 if not set)
    const getVal = (name, def) => parseFloat(document.querySelector(`input[name="${name}"]`)?.value) || def;

    const tdStock = getVal('tax_deferred_stock_percent', 60);
    const tdCash = getVal('tax_deferred_cash_percent', 0);
    const tdBond = 100 - tdStock - tdCash;

    const rothStock = getVal('roth_stock_percent', 60);
    const rothCash = getVal('roth_cash_percent', 0);
    const rothBond = 100 - rothStock - rothCash;

    const taxStock = getVal('taxable_stock_percent', 60);
    const taxCash = getVal('taxable_cash_percent', 0);
    const taxBond = 100 - taxStock - taxCash;

    // Calculate blended returns per account
    const blendReturn = (stock, bond, cash) =>
        (stock/100)*stockMean + (bond/100)*bondMean + (cash/100)*cashMean;

    const tdReturn = blendReturn(tdStock, tdBond, tdCash);
    const rothReturn = blendReturn(rothStock, rothBond, rothCash);
    const taxReturn = blendReturn(taxStock, taxBond, taxCash);

    // Weighted average
    return (tdPercent/100)*tdReturn + (rothPercent/100)*rothReturn + (taxPercent/100)*taxReturn;
}

function toggleSpendingPreview() {
    const panel = document.getElementById('spending-preview-panel');
    const toggleText = document.getElementById('spending-preview-toggle-text');
    if (panel.classList.contains('hidden')) {
        panel.classList.remove('hidden');
        toggleText.textContent = 'Hide Impact';
        updateSpendingPreview();
    } else {
        panel.classList.add('hidden');
        toggleText.textContent = 'Show Impact';
    }
}

function updateSpendingPreview() {
    // The panel and #spending-decline-slider only render when phase-based
    // spending is off. The inflation and monthly-living-expenses sliders call
    // this on every input event either way, so a missing panel is expected.
    const panel = document.getElementById('spending-preview-panel');
    if (!panel || panel.classList.contains('hidden')) return;

    const inflationSlider = document.getElementById('inflation-rate-slider');
    const declineSlider = document.getElementById('spending-decline-slider');
    // Canonical exact value (hidden field), not the step=100-snapped
    // visible range — otherwise this preview can be off by up to $50 from
    // the saved/dragged monthly living expenses figure.
    const expensesSlider = document.getElementById('monthly_living_expenses_value');

    const inflation = parseFloat(inflationSlider.value);
    const decline = parseFloat(declineSlider.value);
    const baseExpenses = parseFloat(expensesSlider.value);
    const netRate = inflation - decline;

    // Update net rate display
    document.getElementById('net-rate-display').textContent = netRate.toFixed(1) + '%';

    // Generate preview table
    const years = [5, 10, 15, 20, 25, 30];
    const tbody = document.getElementById('spending-preview-body');
    tbody.innerHTML = '';

    years.forEach(year => {
        const factor = Math.pow(1 + netRate/100, year);
        const spending = baseExpenses * factor;
        const pctChange = (factor - 1) * 100;

        const row = document.createElement('tr');
        row.className = 'border-t border-gray-200 dark:border-gray-600';

        const sign = pctChange >= 0 ? '+' : '';
        const colorClass = pctChange < 0 ? 'text-positive' :
                          pctChange > 20 ? 'text-negative' : '';

        row.innerHTML = `
            <td class="py-1">Year ${year}</td>
            <td class="text-right py-1">${formatWholeDollars(spending)}/mo</td>
            <td class="text-right py-1 ${colorClass}">${sign}${pctChange.toFixed(0)}%</td>
        `;
        tbody.appendChild(row);
    });
}

// The add-person / remove-person / toggle-preview buttons used to carry
// inline onclick= (U7). Remove is delegated off document since rows are
// created dynamically by addPersonRow() above.
document.addEventListener('DOMContentLoaded', function () {
    var addBtn = document.getElementById('add-person-row-btn');
    if (addBtn) addBtn.addEventListener('click', addPersonRow);
    var toggleBtn = document.getElementById('toggle-spending-preview-btn');
    if (toggleBtn) toggleBtn.addEventListener('click', toggleSpendingPreview);
});

document.addEventListener('click', function (e) {
    var btn = e.target.closest('[data-remove-person-row]');
    if (btn) removePersonRow(btn);
});
