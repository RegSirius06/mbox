"""The public Android home reuses the existing safe subscription refresh path."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"
UI = (JAVA / "MainActivity.java").read_text()
TRANSLATIONS = (JAVA / "UiLanguage.java").read_text()


def test_public_home_has_three_primary_actions_and_no_new_network_client():
    compact = UI.split("if (compactUi) {", 1)[1].split("} else {", 1)[0]
    assert "root.addView(connectButton" in compact
    assert "root.addView(disconnectButton" in compact
    assert "new Button[]{updateProfileButton, settings}" in compact
    assert "root.addView(secondary, layout)" in compact
    assert "root.addView(clipboardButton" not in compact
    assert "root.addView(importButton" not in compact


def test_refresh_uses_existing_identity_checked_subscription_store():
    action = UI.split("private void updateActiveProfile()", 1)[1].split("private void showSettingsHome()", 1)[0]
    assert "subscriptionConnecting" in action
    assert "MBoxVpnService.State.STOPPED" in action
    assert "Disconnect VPN before changing profiles" in action
    assert "SubscriptionStore.activeId(this)" in action
    assert "showImportDialog()" in action
    assert "refreshOneSubscription(id)" in action
    existing = UI.split("private void refreshOneSubscription(String id)", 1)[1].split("private void refreshSubscription(boolean notify)", 1)[0]
    assert "SubscriptionStore.refreshOneForUi(this,id)" in existing
    assert "SubscriptionStore.applySelected(this)" in existing
    store = (JAVA / "SubscriptionStore.java").read_text()
    assert "checkIdentity(c,id,doc);" in store


def test_home_hides_protocol_phase_after_connection():
    connected = UI.split("case CONNECTED:", 1)[1].split("case FAILED:", 1)[0]
    assert 'status.setText(ui("Connected via ") + MBoxVpnService.getActiveTransport());' in connected
    assert "getConnectionPhase()" not in connected
    assert '"\\nQUIC DATAGRAM: "' in UI  # diagnostic still contains full transport detail


def test_ru_and_en_labels_and_busy_state():
    for label in ("Update profile", "Updating profile…", "Import profile"):
        assert f'{{"{label}",' in TRANSLATIONS
    assert "String label = ui(action);" in UI
    assert "updateProfileButton.setEnabled(!subscriptionConnecting);" in UI
