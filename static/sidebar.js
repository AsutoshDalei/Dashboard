(function () {
    var sidebar = document.querySelector('.sidebar');
    var toggle = document.querySelector('.sidebar-toggle');
    var layout = document.querySelector('.dashboard-layout');
    var html = document.documentElement;
    var backdrop = document.querySelector('.sidebar-backdrop');

    if (!sidebar || !toggle) return;

    toggle.title = 'Toggle sidebar';

    var COLLAPSE_KEY = 'sidebar_collapsed';

    function isMobile() {
        return window.innerWidth <= 768;
    }

    function setOpen(open) {
        sidebar.classList.toggle('open', open);
        if (backdrop) backdrop.classList.toggle('show', open);
        if (toggle) toggle.setAttribute('aria-expanded', String(open));
    }

    function setCollapsed(collapsed) {
        if (isMobile()) {
            sidebar.classList.remove('collapsed');
            if (layout) layout.classList.remove('sidebar-collapsed');
            html.setAttribute('data-sidebar', 'expanded');
            setOpen(false);
            return;
        }

        sidebar.classList.toggle('collapsed', collapsed);
        if (layout) layout.classList.toggle('sidebar-collapsed', collapsed);
        html.setAttribute('data-sidebar', collapsed ? 'collapsed' : 'expanded');
        localStorage.setItem(COLLAPSE_KEY, String(collapsed));
        if (toggle) toggle.setAttribute('aria-expanded', String(!collapsed));
    }

    function loadCollapsedState() {
        if (!isMobile() && (html.getAttribute('data-sidebar') === 'collapsed' || localStorage.getItem(COLLAPSE_KEY) === 'true')) {
            setCollapsed(true);
        } else {
            setCollapsed(false);
        }
    }

    loadCollapsedState();

    toggle.addEventListener('click', function () {
        if (isMobile()) {
            setOpen(!sidebar.classList.contains('open'));
        } else {
            setCollapsed(!sidebar.classList.contains('collapsed'));
        }
    });

    if (backdrop) {
        backdrop.addEventListener('click', function () {
            setOpen(false);
        });
    }

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') {
            setOpen(false);
        }
    });

    sidebar.querySelectorAll('.nav-link').forEach(function (a) {
        a.addEventListener('click', function () {
            if (isMobile()) {
                setOpen(false);
            }
        });
    });

    window.addEventListener('resize', function () {
        if (isMobile()) {
            sidebar.classList.remove('collapsed');
            if (layout) layout.classList.remove('sidebar-collapsed');
        } else {
            setOpen(false);
            loadCollapsedState();
        }
    });
})();

window.escapeHtml = function escapeHtml(text) {
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
};
