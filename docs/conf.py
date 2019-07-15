
import sphinx_rtd_theme

project = u'shipper'
copyright = u'2019, Booking.com'

source_suffix = '.rst'

master_doc = 'index'

templates_path = ['_templates']

html_theme = 'sphinx_rtd_theme'

html_theme_path = [sphinx_rtd_theme.get_html_theme_path()]

html_static_path = ['_static']

html_context = {
    'css_files': [
        '_static/theme_overrides.css',  # override wide tables in RTD theme
    ],
}
