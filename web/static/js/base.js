// Base layout: HTMX request/swap wiring (scroll-position preservation on
// #whatif-results swaps, error logging), theme-toggle buttons, mobile nav
// toggle. Extracted from layouts/base.html (U7).

        (function () {
            // Configure HTMX
            document.body.addEventListener('htmx:configRequest', function (evt) {
                // Add any custom headers here if needed
            });

            // Prevent scroll-jump when swapping #whatif-results (slider changes, etc.)
            (function () {
                var savedScroll = null;
                document.body.addEventListener('htmx:beforeSwap', function (evt) {
                    if (evt.detail.xhr && evt.detail.xhr.status >= 400 && evt.detail.xhr.status < 500 &&
                        evt.detail.xhr.getResponseHeader('HX-Retarget')) {
                        evt.detail.shouldSwap = true;
                        evt.detail.isError = false;
                    }
                    if (evt.detail.target && evt.detail.target.id === 'whatif-results') {
                        savedScroll = window.scrollY || document.documentElement.scrollTop;
                    }
                });
                document.body.addEventListener('htmx:afterSwap', function (evt) {
                    if (savedScroll !== null && evt.detail.target && evt.detail.target.id === 'whatif-results') {
                        window.scrollTo({ top: savedScroll, behavior: 'instant' });
                        savedScroll = null;
                    }
                });
            })();

            // Handle HTMX errors
            document.body.addEventListener('htmx:responseError', function (evt) {
                console.error('HTMX error:', evt.detail);
            });

            // Theme toggle functionality
            ['theme-toggle', 'theme-toggle-mobile'].forEach(function (id) {
                var btn = document.getElementById(id);
                if (!btn || btn._listenerAttached) return;
                btn._listenerAttached = true;
                btn.addEventListener('click', function () {
                    var html = document.documentElement;
                    var isDark = html.classList.contains('dark');
                    if (isDark) {
                        html.classList.remove('dark');
                        html.classList.add('light');
                        localStorage.setItem('theme', 'light');
                    } else {
                        html.classList.remove('light');
                        html.classList.add('dark');
                        localStorage.setItem('theme', 'dark');
                    }
                    window.dispatchEvent(new CustomEvent('themechange', { detail: { dark: !isDark } }));
                });
            });

            var navToggle = document.getElementById('mobile-nav-toggle');
            var mobileNav = document.getElementById('mobile-nav');
            if (navToggle && mobileNav && !navToggle._listenerAttached) {
                navToggle._listenerAttached = true;
                navToggle.addEventListener('click', function () {
                    var open = mobileNav.classList.toggle('hidden') === false;
                    navToggle.setAttribute('aria-expanded', open ? 'true' : 'false');
                });
            }
        })();
