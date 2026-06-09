(function () {
    var sidebar = document.querySelector('.sidebar');
    var toggle = document.querySelector('.sidebar-toggle');
    var layout = document.querySelector('.dashboard-layout');
    var html = document.documentElement;

    if (!sidebar || !toggle) return;

    toggle.title = 'Toggle sidebar';

    var COLLAPSE_KEY = 'sidebar_collapsed';

    function setCollapsed(collapsed) {
        if (collapsed) {
            sidebar.classList.add('collapsed');
            if (layout) layout.classList.add('sidebar-collapsed');
            html.setAttribute('data-sidebar', 'collapsed');
            localStorage.setItem(COLLAPSE_KEY, 'true');
        } else {
            sidebar.classList.remove('collapsed');
            if (layout) layout.classList.remove('sidebar-collapsed');
            html.removeAttribute('data-sidebar');
            localStorage.setItem(COLLAPSE_KEY, 'false');
        }
    }

    // Load saved state (data-sidebar already set by inline <head> script)
    if (html.getAttribute('data-sidebar') === 'collapsed') {
        setCollapsed(true);
    } else if (localStorage.getItem(COLLAPSE_KEY) === 'true') {
        setCollapsed(true);
    }

    toggle.addEventListener('click', function () {
        var isMobile = window.innerWidth <= 768;

        if (isMobile) {
            sidebar.classList.toggle('open');
            var backdrop = document.querySelector('.sidebar-backdrop');
            if (backdrop) {
                backdrop.classList.toggle('show');
            }
            toggle.setAttribute('aria-expanded', sidebar.classList.contains('open'));
        } else {
            setCollapsed(!sidebar.classList.contains('collapsed'));
        }
    });

    var backdrop = document.querySelector('.sidebar-backdrop');
    if (backdrop) {
        backdrop.addEventListener('click', function () {
            sidebar.classList.remove('open');
            backdrop.classList.remove('show');
            toggle.setAttribute('aria-expanded', 'false');
        });
    }

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') {
            sidebar.classList.remove('open');
            if (backdrop) backdrop.classList.remove('show');
            toggle.setAttribute('aria-expanded', 'false');
        }
    });

    sidebar.querySelectorAll('.nav-link').forEach(function (a) {
        a.addEventListener('click', function () {
            if (window.innerWidth <= 768) {
                sidebar.classList.remove('open');
                if (backdrop) backdrop.classList.remove('show');
                toggle.setAttribute('aria-expanded', 'false');
            }
        });
    });

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