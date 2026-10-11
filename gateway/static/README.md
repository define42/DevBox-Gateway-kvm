# Gateway interface assets

`theme.css`, `dashboard.css`, and the Bootstrap and Bootstrap Icons distributions
are copied from [DevBox Gateway](https://github.com/define42/DevBox-Gateway/tree/e4fbf205adb7663167190f1082c41ff81681d3d8/internal/webassets).
The login and dashboard templates use the same layout and component classes,
with gokvm's authenticated forms and supported VM actions.

The upstream MIT license is in `LICENSE-DevBox-Gateway`. Each vendored library
includes its own license. All styles, scripts and icon fonts are served locally;
no CDN or frontend build step is required. `gokvm.css` contains the small layout
and accessibility adaptations for the native dashboard.
