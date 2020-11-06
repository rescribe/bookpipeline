// Copyright 2020 Nick White.
// Use of this source code is governed by the GPLv3
// license that can be found in the LICENSE file.

/*
The bookpipeline package contains various tools and functions for the OCR of
books, with a focus on distributed OCR using short-lived virtual servers.
It also contains several tools that are useful standalone; read the
accompanying README for more details.

Introduction

The book pipeline is a way to split the different processes that for book OCR
into small jobs, which can be processed when a computer is ready for them. It
is currently implemented with Amazon's AWS cloud systems, and can scale from
zero to many computers, with jobs being processed faster when more servers are
available.

Central to the bookpipeline in terms of software is the bookpipeline command,
which is part of the rescribe.xyz/bookpipeline package. Presuming you have the
go tools installed, you can install it, and useful tools to control the
system, with this command:
  go get -u rescribe.xyz/bookpipeline/...

All of the tools provided in the bookpipeline package will give information on
what they do and how they work with the '-h' flag, so for example to get usage
information on the booktopipeline tool simply run the following:
  booktopipeline -h

To get the pipeline tools to work for you, you'll need to change the settings
in cloudsettings.go, and set up your ~/.aws/credentials appropriately.

Managing servers

Most of the time the bookpipeline is expected to be run from potentially
short-lived servers on Amazon's EC2 system. EC2 provides servers which have
no guaranteed of stability (though in practice they seem to be), called "Spot
Instances", which we use for bookpipeline. bookpipeline can handle a process
or server being suddenly destroyed without warning (more on this later), so
Spot Instances are perfect for us. We have set up a machine image with
bookpipeline preinstalled which will launch at bootup, which is all that's
needed to launch an bookpipeline instance. Presuming the bookpipeline package
has been installed on your computer (see above), the spot instance can be
started with the command:
  spotme

You can keep an eye on the servers (spot or otherwise) that are running, and
the jobs left to do and in progress, with the "lspipeline" tool (which is
also part of the bookpipeline package). It's recommended to use this with
the ssh private key for the servers, so that it can also report on what each
server is currently doing, but it can run successfully without it. It takes
a little while to run, so be patient. It can be run with the command:
  lspipeline -i key.pem

Spot instances can be terminated with ssh, using their ip address which can
be found with lspipeline, like so:
  ssh -i key.pem admin@<ip-address> sudo poweroff

The bookpipeline program is run as a service managed by systemd on the
servers. The system is fully resiliant in the face of unexpected failures.
See the section "How the pipeline works" for details on this. bookpipeline
can be managed like any other systemd service. A few examples:
  # show all logs for bookpipeline:
  ssh -i key.pem admin@<ip-address> journalctl -n all -u bookpipeline
  # restart bookpipeline
  ssh -i key.pem admin@<ip-address> systemctl restart bookpipeline

Using the pipeline

Books can be added to the pipeline using the "booktopipeline" tool. This
takes a directory of page images as input, and uploads them all to S3, adding
a job to the pipeline queue to start processing them. So it can be used like
this:
  booktopipeline -v ExcellentBook/

Getting a finished book

Once a book has been finished, it can be downloaded using the
"getpipelinebook" tool. This has several options to download specific parts
of a book, but the default case will download the best hOCR for each page,
PDFs, and the best, conf and graph.png files. Use it like this:
  getpipelinebook ExcellentBook

To get the plain text from the book, use the hocrtotxt tool, which is part
of the rescribe.xyz/utils package. You can get the package, and run the tool,
like this:
  go get -u rescribe.xyz/utils/...
  hocrtotext ExcellentBook/0010_bin0.2.hocr > ExcellentBook/0010_bin0.2.txt

How the pipeline works

The central part of the book pipeline is several SQS queues, which contain
jobs which need to be done by a server running bookpipeline. The exact content
of the SQS messages vary according to each queue, as some jobs need more
information than others. Each queue is checked at least once every couple of
minutes on any server that isn't currently processing a job.

When a job is taken from the queue by a process, it is hidden from the queue
for 2 minutes so that no other process can take it. Once per minute when
processing a job the process sends a message updating the queue, to tell it
to keep the job hidden for two minutes. This is called the "heartbeat", as if
the process fails for any reason the heartbeat will stop, and in 2 minutes
the job will reappear on the queue for another process to have a go at. Once
a job is completed successfully it is deleted from the queue.

Queues

Queue names are defined in cloudsettings.go.

queuePreProc

Each message in the queuePreProc queue is a bookname, optionally
followed by a space and the name of the training to use. Each page of the
bookname will be binarised with several different parameters, and then
wiped, with each version uploaded to S3, with the path of the preprocessed
page, plus the training name if it was provided, will be added to the
queueOcrPage queue. The pages are binarised with different parameters as
it can be difficult to determine which binarisation level will be best prior
to OCR, so several different options are used, and in the queueAnalyse
step the best one is chosen, based on the confidence of the OCR output.

  example message: APolishGentleman_MemoirByAdamKruczkiewicz
  example message: APolishGentleman_MemoirByAdamKruczkiewicz rescribelatv7

queueWipeOnly

This queue works the same as queuePreProc, except that it doesn't
binarise the pages, only runs the wiper. Hence it is designed for books
which have been prebinarised.

  example message: APolishGentleman_MemoirByAdamKruczkiewicz
  example message: APolishGentleman_MemoirByAdamKruczkiewicz rescribefrav2

queueOcrPage

This queue contains the path of individual pages, optionally followed by
a space and the name of the training to use. Each page is OCRed, and the
results are uploaded to S3. After each page is OCRed, a check is made to
see whether all pages that look like they were preprocessed have
corresponding .hocr files. If so, the bookname is added to the
queueAnalyse queue.

  example message: APolishGentleman_MemoirByAdamKruczkiewicz/00162_bin0.0.png
  example message: APolishGentleman_MemoirByAdamKruczkiewicz/00162_bin0.0.png rescribelatv7

queueAnalyse

A message on the queueAnalyse queue contains only a book name. The
confidences for each page are calculated and saved in the 'conf' file, and
the best version of each page is decided upon and saved in the 'best' file.
PDFs are then generated, and the confidence graph is generated.

  example message: APolishGentleman_MemoirByAdamKruczkiewicz

Queue manipulation

The queues should generally only be messed with by the bookpipeline and
booktopipeline tools, but if you're feeling ambitious you can take a look at
the `addtoqueue` tool.

Remember that messages in a queue are hidden for a few minutes when they are
read, so for example you couldn't straightforwardly delete a message which was
currently being processed by a server, as you wouldn't be able to see it.

Page naming

At present the bookpipeline has some silly limitations of file names for book
pages to be recognised. This is something which will be fixed in due course.
  Pages that are to be fully processed: *[0-9]{4}.jpg$
  Pages that are to be wiped only: *[0-9]{6}(.bin)?.png$

Local operation

While bookpipeline was built with cloud based operation in mind, there is also
a local mode that can be used to run OCR jobs from a single computer, with all
the benefits of preprocessing, choosing the best threshold for each image,
graph creation and so on that the pipeline provides.

You can use this by passing the '-c local' flag to the core bookpipeline
commands. Here is a simple example run:

  booktopipeline -c local MyBook
  bookpipeline -v -c local           # run until MyBook has finished processing
  getpipelinebook -c local MyBook

Note that the local mode is not as well tested as the core cloud modes; please
report any bugs you find with it.
*/
package bookpipeline
