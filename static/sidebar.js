(function () {
    const sidebar = document.querySelector('.sidebar');
    const toggle = document.querySelector('.sidebar-toggle');
    const backdrop = document.querySelector('.sidebar-backdrop');
    if (!sidebar || !toggle || !backdrop) return;

    const open = () => {
        sidebar.classList.add('open');
        backdrop.classList.add('show');
        toggle.setAttribute('aria-expanded', 'true');
    };
    const close = () => {
        sidebar.classList.remove('open');
        backdrop.classList.remove('show');
        toggle.setAttribute('aria-expanded', 'false');
    };

    toggle.addEventListener('click', () => {
        sidebar.classList.contains('open') ? close() : open();
    });
    backdrop.addEventListener('click', close);
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') close();
    });
    sidebar.querySelectorAll('.nav-link').forEach((a) => {
        a.addEventListener('click', close);
    });
    window.addEventListener('resize', () => {
        if (window.innerWidth > 768) close();
    });
})();
