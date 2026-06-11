(function () {
    var sidebar = document.querySelector('.sidebar');
    var toggles = Array.prototype.slice.call(document.querySelectorAll('.sidebar-toggle, .mobile-menu-toggle'));
    var toggle = toggles[0];
    var mobileToggles = Array.prototype.slice.call(document.querySelectorAll('.mobile-menu-toggle'));
    var layout = document.querySelector('.dashboard-layout');
    var html = document.documentElement;
    var backdrop = document.querySelector('.sidebar-backdrop');

    if (!sidebar || !toggle) return;

    toggles.forEach(function (btn) {
        btn.title = 'Toggle sidebar';
    });

    var COLLAPSE_KEY = 'sidebar_collapsed';

    function isMobile() {
        return window.matchMedia('(max-width: 767.98px)').matches;
    }

    function setOpen(open) {
        sidebar.classList.toggle('open', open);
        if (backdrop) backdrop.classList.toggle('show', open);
        toggles.forEach(function (btn) {
            btn.setAttribute('aria-expanded', String(open));
            btn.setAttribute('aria-label', open ? 'Close menu' : 'Open menu');
        });
        mobileToggles.forEach(function (btn) {
            btn.classList.toggle('mobile-menu-hidden', open);
        });
    }

    function closeMobileSidebar() {
        sidebar.classList.remove('open', 'collapsed');
        if (layout) layout.classList.remove('sidebar-collapsed');
        if (backdrop) backdrop.classList.remove('show');
        toggles.forEach(function (btn) {
            btn.setAttribute('aria-expanded', 'false');
            btn.setAttribute('aria-label', 'Open menu');
        });
    }

    function setCollapsed(collapsed) {
        if (isMobile()) {
            closeMobileSidebar();
            return;
        }

        sidebar.classList.toggle('collapsed', collapsed);
        if (layout) layout.classList.toggle('sidebar-collapsed', collapsed);
        html.setAttribute('data-sidebar', collapsed ? 'collapsed' : 'expanded');
        localStorage.setItem(COLLAPSE_KEY, String(collapsed));
        toggles.forEach(function (btn) {
            btn.setAttribute('aria-expanded', String(!collapsed));
        });
    }

    function loadCollapsedState() {
        if (!isMobile() && (html.getAttribute('data-sidebar') === 'collapsed' || localStorage.getItem(COLLAPSE_KEY) === 'true')) {
            setCollapsed(true);
        } else {
            setCollapsed(false);
        }
    }

    loadCollapsedState();

    toggles.forEach(function (btn) {
        btn.addEventListener('click', function () {
            if (isMobile()) {
                sidebar.classList.remove('collapsed');
                if (layout) layout.classList.remove('sidebar-collapsed');
                setOpen(!sidebar.classList.contains('open'));
            } else {
                setCollapsed(!sidebar.classList.contains('collapsed'));
            }
        });
    });

    if (backdrop) {
        backdrop.addEventListener('click', function () {
            closeMobileSidebar();
        });
    }

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') {
            closeMobileSidebar();
        }
    });

    sidebar.querySelectorAll('.nav-link').forEach(function (a) {
        a.addEventListener('click', function () {
            if (isMobile()) {
                closeMobileSidebar();
            }
        });
    });

    window.addEventListener('resize', function () {
        if (isMobile()) {
            closeMobileSidebar();
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
