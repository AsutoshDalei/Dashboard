(function () {
    var sidebar = document.querySelector('.sidebar');
    var toggle = document.querySelector('.sidebar-toggle');
    var layout = document.querySelector('.dashboard-layout');

    if (!sidebar || !toggle) return;

    var COLLAPSE_KEY = 'sidebar_collapsed';

    // Load saved state
    if (localStorage.getItem(COLLAPSE_KEY) === 'true') {
        sidebar.classList.add('collapsed');
        if (layout) layout.classList.add('sidebar-collapsed');
    }

    toggle.addEventListener('click', function () {
        var wasCollapsed = sidebar.classList.contains('collapsed');
        var isMobile = window.innerWidth <= 768;

        if (isMobile) {
            // Mobile: toggle open/close (overlay behavior)
            sidebar.classList.toggle('open');
            var backdrop = document.querySelector('.sidebar-backdrop');
            if (backdrop) {
                backdrop.classList.toggle('show');
            }
            toggle.setAttribute('aria-expanded', sidebar.classList.contains('open'));
        } else {
            // Desktop: toggle collapsed
            if (wasCollapsed) {
                sidebar.classList.remove('collapsed');
                if (layout) layout.classList.remove('sidebar-collapsed');
                localStorage.setItem(COLLAPSE_KEY, 'false');
            } else {
                sidebar.classList.add('collapsed');
                if (layout) layout.classList.add('sidebar-collapsed');
                localStorage.setItem(COLLAPSE_KEY, 'true');
            }
        }
    });

    // Backdrop click closes on mobile
    var backdrop = document.querySelector('.sidebar-backdrop');
    if (backdrop) {
        backdrop.addEventListener('click', function () {
            sidebar.classList.remove('open');
            backdrop.classList.remove('show');
            toggle.setAttribute('aria-expanded', 'false');
        });
    }

    // Escape key closes on mobile
    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') {
            sidebar.classList.remove('open');
            if (backdrop) backdrop.classList.remove('show');
            toggle.setAttribute('aria-expanded', 'false');
        }
    });

    // Close nav links on mobile after click
    sidebar.querySelectorAll('.nav-link').forEach(function (a) {
        a.addEventListener('click', function () {
            if (window.innerWidth <= 768) {
                sidebar.classList.remove('open');
                if (backdrop) backdrop.classList.remove('show');
                toggle.setAttribute('aria-expanded', 'false');
            }
        });
    });

    // Reset on resize
    window.addEventListener('resize', function () {
        if (window.innerWidth > 768) {
            sidebar.classList.remove('open');
            if (backdrop) backdrop.classList.remove('show');
            toggle.setAttribute('aria-expanded', 'false');
        }
    });
})();

window.escapeHtml = function escapeHtml(text) {
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
};