# PowerPilot UI System

`app/ui_system.go` is the single source of truth for shared form geometry.

## Rules
1. Do not hard-code a separate width for text before an inline numeric field.
   Use `uiInlineNumberLayout` + `uiDrawInlineNumber`.
2. Do not position the same kind of native EDIT differently on different pages.
   Use `uiPlaceCompactEdit` / `uiPlaceInlineNumberEdit`.
3. Simple task, Advanced task and Saved-task `When` editors must use
   `uiLayoutWhenFields` / `uiLayoutWhenFieldsAt`.
4. Shared vertical spacing comes from `uiMetricsDefault`.
5. New reusable form patterns should be implemented in `ui_system.go` first and then consumed by pages.

This prevents local “+37 px / width=104” fixes from drifting apart across the application.

## 0.3.7 additions

- Text used for geometry is measured by DirectWrite, the same renderer used on screen. Do not add GDI-only text measurement to page layouts.
- Inline numeric sentences use one canonical gap and compact height from `uiMetricsDefault`; never hard-code prefix widths.
- Toggle-style settings rows use `uiSettingsRowTop` / `uiInlineSentenceY`.
- Dropdown content must push interactive page controls out of its occupied area instead of relying on z-order overlap.
