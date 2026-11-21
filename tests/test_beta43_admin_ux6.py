"""Read-only, UI-only guard for the beta.4.3 admin redesign."""
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
HTML=(ROOT/'tools/mbox-admin/index.html').read_text(encoding='utf-8')


def test_admin_ux6_preserves_actions_and_csrf():
    assert 'MBOX_BETA43_ADMIN_UX6' in HTML
    for needle in ("req('/api/users'", "req('/api/action'", "req('/api/settings'",
                   "'X-MBox-CSRF'", "confirm('Применить", "confirm:'APPLY'",
                   'id="cfgSave"', 'id="userrows"', 'id="secretbox"'):
        assert needle in HTML


def test_admin_ux6_observes_only_existing_safe_metrics():
    assert 'function mboxUpdateOverview(users)' in HTML
    assert 'mboxUpdateOverview(data.users)' in HTML
    assert 'user.id ??' in HTML
    assert 'user.online' in HTML
    assert 'Не тарифная статистика REG.RU.' in HTML
    assert 'Автоматического опроса нет.' in HTML
    assert 'setInterval(' not in HTML
    assert 'mboxAdminSelectTab(' in HTML


def test_admin_ux6_sections_and_mobile():
    for section in ('overview','clients','subscriptions','configuration','diagnostics'):
        assert f'data-mbox-panel="{section}"' in HTML
        assert f'data-mbox-tab="{section}"' in HTML
    assert '@media(max-width:800px)' in HTML
    assert 'class="mbox-rail"' in HTML
