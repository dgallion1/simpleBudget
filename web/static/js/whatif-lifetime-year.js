(function () {
  let selectedYear = null;
  let restoreYearFocus = false;

  function showSelected(select) {
    document.querySelectorAll('[data-lifetime-year-panel]').forEach((panel) => {
      panel.hidden = panel.dataset.lifetimeYearPanel !== select.value;
    });
  }

  function bind(root) {
    const select = (root || document).querySelector('#lifetime-year-select') || document.querySelector('#lifetime-year-select');
    if (!select || select.dataset.bound) return;
    if (selectedYear && Array.from(select.options).some((option) => option.value === selectedYear)) {
      select.value = selectedYear;
    } else if (selectedYear && select.options.length) {
      const status = document.querySelector('#lifetime-year-status');
      if (status) status.textContent = `Previously selected year ${selectedYear} is no longer available. Showing ${select.value}.`;
      selectedYear = select.value;
    } else {
      selectedYear = select.value;
    }
    showSelected(select);
    select.dataset.bound = 'true';
    select.addEventListener('change', () => {
      selectedYear = select.value;
      showSelected(select);
    });
    if (restoreYearFocus) {
      select.focus({preventScroll: true});
      restoreYearFocus = false;
    }
  }

  document.addEventListener('DOMContentLoaded', () => bind(document));
  document.addEventListener('htmx:beforeSwap', () => {
    const select = document.querySelector('#lifetime-year-select');
    if (!select) return;
    selectedYear = select.value;
    restoreYearFocus = document.activeElement === select;
  });
  document.addEventListener('htmx:afterSwap', (event) => bind(event.target));
}());
