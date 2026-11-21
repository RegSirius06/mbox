"""Source-layout regression guards; physical UI verification requires Android."""
from pathlib import Path

SOURCE = Path(__file__).resolve().parents[1] / "android/app/src/main/java/io/mbox/android/MainActivity.java"


def test_activity_pages_respect_android_edge_to_edge_insets():
    src = SOURCE.read_text()
    assert src.count("showScrollPage(scroll);") == 2
    assert src.count("setContentView(scroll);") == 1
    helper = src.split("private void showScrollPage(ScrollView scroll)", 1)[1].split("private void addPageAction", 1)[0]
    for token in ("Build.VERSION.SDK_INT >= 35", "setOnApplyWindowInsetsListener",
                  "WindowInsets.Type.systemBars()", "WindowInsets.Type.displayCutout()",
                  "WindowInsets.Type.ime()", "view.setPadding(safe.left, safe.top, safe.right, safe.bottom)"):
        assert token in helper
    assert helper.index("setContentView(scroll);") < helper.index("scroll.requestApplyInsets();")
    assert "getPadding" not in helper  # repeated inset dispatch is idempotent


def test_diagnostics_has_extra_spacing_without_changing_other_page_padding():
    src = SOURCE.read_text()
    page = src.split("private LinearLayout openPage(", 1)[1].split("private void showScrollPage(", 1)[0]
    assert "if (page == Page.DIAGNOSTICS)" in page
    assert "content.setPadding(dp(28), dp(28), dp(28), dp(36))" in page
    assert "content.setPadding(dp(22), dp(18), dp(22), dp(30))" in page
    assert "dp(page == Page.DIAGNOSTICS ? 34 : 26)" in page
    diag = src.split("private void showDiagnostics()", 1)[1].split("private void showLabTools()", 1)[0]
    assert "diagnosticsView.setPadding(dp(22), dp(26), dp(22), dp(26))" in diag
    assert "diagnosticsParams.bottomMargin = dp(18)" in diag
    assert "TransportSettings.show(this)" in diag
