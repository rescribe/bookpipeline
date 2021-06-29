The embedded copies of Tesseract are fetched by `go generate` from
copies that are stored online.

To create them yourself, you need to create a .zip file that contains
the tesseract executable, plus any libraries that are needed for it
to run.

It must be linked so that these libraries are accessed from the same
directory as the executable. On Windows this is the default
behaviour. On Linux we just create a static binary using the
simplemake branch of Tesseract, available at
https://github.com/nickjwhite/tesseract

On OSX it's a bit more complicated. We install Tesseract on a host
machine using homebrew, then copy the binary, and run
`otool -L tesseract` to find the libraries that need to be copied
as well. Then `otool -L libname.dylib` needs to be run for each
library to find all non-system libraries they depend on, to copy.
Once that is done, `install_name_tool` needs to be run on the
binary and libraries to set the lookup path to the local directory,
like this:
  install_name_tool -change /usr/local/opt/libpng/lib/libpng16.16.dylib @executable_path/libpng16.16.dylib liblept.5.dylib
You can find the path names to replace using `otool -L`.
This is all taken from a great guide on how to do this:
http://thecourtsofchaos.com/2013/09/16/how-to-copy-and-relink-binaries-on-osx/

The embedded tessdata is much easier to create, it's just a
standard tessdata from an install on any platform, plus any
additional .traineddata files you want to include.
