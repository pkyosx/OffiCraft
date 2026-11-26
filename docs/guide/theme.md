# Themes (look and wording)

After reading this page, you will know which built-in themes exist, how switching by time of day and seasonal themes work, how background animation plays, which parts of a custom theme you can change, and the rules for importing and exporting themes.

A theme changes only the look and the wording. It does not change where things are on the screen, and it does not change the language. Themes and language are independent of each other; for example, you can use "Office · Light (no background)" with English.

## Choosing the appearance

There are two ways into the theme settings: "Theme" under avatar menu › Preferences opens "Appearance"; or press the gear icon in the top bar to go to **Settings › Theme**. That page has two sections, "Appearance" at the top and "Theme library" below. Adding, importing, exporting, and editing themes are done in "Theme library".

There are five built-in themes: "Office · Morning light", "Office · Dusk", and "Office · Night lamp" have an office background; "Office · Dark (no background)" and "Office · Light (no background)" have colors only. The theme does not switch automatically to follow your computer's dark or light mode setting.

Pressing "Restore default" changes back to the default appearance of a new studio, and also changes background animation on this device back to "Auto". Custom themes are not deleted.

## Changing themes automatically by time of day

A newly created studio switches themes by time of day by default: Office · Morning light at 06:00, Office · Dusk at 16:00, and Office · Night lamp at 18:30. When upgrading from an older version, studios that used the dark or light office theme, or had never chosen a theme, are also changed to this default schedule. This conversion happens only once.

You can change the time slots and the theme used for each slot. You can also turn off "Switch by time of day" and use one fixed theme. Times are based on the local time of the device you are using; the setting is saved in the studio, so it is the same on every device.

## Turning off seasonal themes

During holidays (for example Halloween or Thanksgiving), the console automatically switches to a seasonal theme that replaces the colors, avatars, logo, navigation icons, and background as a set. Some seasonal themes also switch to a daytime, dusk, or night version depending on the time of day. "Office" and the "Staff" and "Outsource" tabs inside Office may be renamed for the holiday. All other wording stays the same as usual, except that a holiday emoji or a short seasonal phrase is added after the navigation, the headings, and the hints in empty lists. When the holiday period ends, the console automatically switches back to the original theme, without a page reload.

When "Show seasonal theme" in the theme settings is set to "Off", the console does not switch to seasonal themes.

## Stopping the background animation

For an animated background, you can choose how it plays under "Background animation" in the theme settings:

- **Auto** (default): touch devices such as phones and tablets show a still image of the background's first frame; computers used with a mouse or trackpad play the animation.
- **Play**, **Still**: always play, or always stay still, on every device.

"Background animation" is stored only in this device's browser. It does not sync to other devices with your account.

## Making your own theme

"New" in **Settings › Theme** creates a custom theme and opens the editor. The new theme is based on Office · Dark (no background), and its name starts as "New theme".

In the editor you can change the name, colors, wording, fonts, avatars, studio logo, navigation icons, and outer canvas. Everything except colors can be left blank; anything left blank uses the built-in design.

A saved custom theme is not applied automatically. It is applied only when you choose it in the theme settings or put it into a time slot.

Built-in themes can also be exported. An exported file becomes a custom theme after it is imported, and can then be edited.

For what each section can change, the size limits for images, and how the outer canvas is laid out, see "Theme" in [Settings and parameters](settings.md).

## Exporting and importing

**Export**: press "Export" on a row in **Settings › Theme** to get a `.json` theme file.

**Import**: press "Import", then do one of three things: paste the content of a theme file, choose a `.json` file, or paste a link to a theme file.

Before importing, the system checks the content. Invalid color values, arbitrary code, and content that is too large are all blocked, so importing a theme file someone else gave you is safe.

**Be careful when sharing a theme by link**: anyone who has the link can see the theme, including any private images in it. The link does not expire and cannot be revoked on its own.

For what an exported file contains and what the messages shown during import mean, see "Theme" in [Settings and parameters](settings.md).

## Related pages

- [Settings and parameters](settings.md)
- [Interface reference](interface.md)
- [Glossary](glossary.md)
