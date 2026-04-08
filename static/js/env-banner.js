// Display environment banner for non-production deployments
(function() {
    var host = window.location.hostname;
    var label;
    if (host === 'staging.communityrapidresponse.net') {
        label = 'STAGING';
    } else if (host === 'communityrapidresponse.net') {
        label = null;
    } else {
        label = 'DEV';
    }
    if (label) {
        var banner = document.createElement('div');
        banner.id = 'env-banner';
        banner.textContent = label;
        banner.style.cssText = 'position:sticky;top:0;z-index:9999;background:#d32f2f;color:#fff;text-align:center;padding:4px 0;font-weight:700;font-size:13px;letter-spacing:2px;';
        document.body.prepend(banner);
    }
})();
